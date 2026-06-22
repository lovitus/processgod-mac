package config

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/lovitus/processgod-mac/internal/api"
)

var ErrRevisionConflict = errors.New("config revision conflict")
var ErrRevisionExhausted = errors.New("config revision exhausted")

type Store struct {
	mu      sync.RWMutex
	path    string
	config  Config
	loadErr error
}

func OpenStore(path string) *Store {
	store := &Store{path: path}
	cfg, err := Load(path)
	if err != nil {
		store.config = Config{SchemaVersion: CurrentSchemaVersion, Revision: 1, PathEnv: DefaultPathEnv(), Items: []Item{}}
		store.loadErr = err
		return store
	}
	store.config = cfg
	return store
}

func (s *Store) Path() string { return s.path }

func (s *Store) Snapshot() (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config), s.loadErr
}

func (s *Store) APISnapshot() (api.ConfigSnapshot, error) {
	cfg, err := s.Snapshot()
	return cfg.Snapshot(), err
}

func (s *Store) Replace(snapshot api.ConfigSnapshot, expectedRevision uint64) (Config, error) {
	cfg, err := FromSnapshot(snapshot)
	if err != nil {
		return Config{}, err
	}
	return s.update(expectedRevision, func(current *Config) error {
		cfg.Revision = current.Revision + 1
		*current = cfg
		return nil
	})
}

// ReplaceConfig imports the storage model so compatibility-only fields survive
// daemon scope migrations without becoming part of the Swift editing model.
func (s *Store) ReplaceConfig(imported Config, expectedRevision uint64) (Config, error) {
	return s.update(expectedRevision, func(current *Config) error {
		if current.Revision == ^uint64(0) {
			return ErrRevisionExhausted
		}
		imported.Revision = current.Revision + 1
		*current = cloneConfig(imported)
		return nil
	})
}

func (s *Store) Update(expectedRevision uint64, mutate func(*Config) error) (Config, error) {
	return s.update(expectedRevision, func(cfg *Config) error {
		if err := mutate(cfg); err != nil {
			return err
		}
		if cfg.Revision == ^uint64(0) {
			return ErrRevisionExhausted
		}
		cfg.Revision++
		return nil
	})
}

// Reload adopts a manually edited file while preserving Store revision
// monotonicity and normalizing it back through the atomic writer.
func (s *Store) Reload() (Config, error) {
	loaded, err := Load(s.path)
	if err != nil {
		return Config{}, err
	}
	if err := Validate(loaded); err != nil {
		return Config{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Revision == ^uint64(0) {
		return Config{}, ErrRevisionExhausted
	}
	loaded.Revision = s.config.Revision + 1
	if err := Save(s.path, loaded); err != nil {
		return Config{}, err
	}
	s.config = loaded
	s.loadErr = nil
	return cloneConfig(loaded), nil
}

func (s *Store) update(expectedRevision uint64, mutate func(*Config) error) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != 0 && s.config.Revision != expectedRevision {
		return Config{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, s.config.Revision)
	}
	next := cloneConfig(s.config)
	if err := mutate(&next); err != nil {
		return Config{}, err
	}
	next.Normalize()
	if err := Validate(next); err != nil {
		return Config{}, err
	}
	if err := Save(s.path, next); err != nil {
		return Config{}, err
	}
	s.config = next
	s.loadErr = nil
	return cloneConfig(next), nil
}

func (s *Store) BackupOnce(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func cloneConfig(cfg Config) Config {
	out := cfg
	out.Items = make([]Item, len(cfg.Items))
	for i, item := range cfg.Items {
		out.Items[i] = item
		if item.Args != nil {
			out.Items[i].Args = append([]string(nil), item.Args...)
		}
		if item.Env != nil {
			out.Items[i].Env = make(map[string]string, len(item.Env))
			for key, value := range item.Env {
				out.Items[i].Env[key] = value
			}
		}
	}
	return out
}
