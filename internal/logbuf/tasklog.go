package logbuf

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lovitus/processgod-mac/internal/api"
)

type Entry struct {
	Seq       int64
	Timestamp time.Time
	Source    string
	Text      string
	Err       bool
}

const MaxStoredLineBytes = 4096

type entryRing struct {
	buf      []Entry
	capacity int
	head     int
	count    int
}

func newEntryRing(capacity int) *entryRing {
	if capacity <= 0 {
		capacity = 1
	}
	return &entryRing{buf: make([]Entry, capacity), capacity: capacity}
}

func (r *entryRing) add(e Entry) {
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
}

func (r *entryRing) entries() []Entry {
	if r.count == 0 {
		return nil
	}
	out := make([]Entry, 0, r.count)
	start := (r.head - r.count + r.capacity) % r.capacity
	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.capacity
		out = append(out, r.buf[idx])
	}
	return out
}

func (r *entryRing) len() int { return r.count }
func (r *entryRing) cap() int { return r.capacity }

// TaskLog keeps separate memory-only buffers for error/warning and standard logs.
type TaskLog struct {
	mu     sync.Mutex
	err    *entryRing
	std    *entryRing
	total  int64
	errCap int
	stdCap int
}

func NewTaskLog(errWarnCapacity, stdCapacity int) *TaskLog {
	return &TaskLog{
		err:    newEntryRing(errWarnCapacity),
		std:    newEntryRing(stdCapacity),
		errCap: errWarnCapacity,
		stdCap: stdCapacity,
	}
}

func (t *TaskLog) Add(line string, stderr bool) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.total == math.MaxInt64 {
		// Keep sequence labels positive if the process runs for an extreme duration.
		t.total = 0
	}
	t.total++
	raw := line
	source := "stdout"
	if stderr {
		source = "stderr"
	}
	e := Entry{Seq: t.total, Timestamp: time.Now().UTC(), Source: source, Text: truncateStoredLine(raw)}
	if stderr || isErrWarnLine(raw) {
		e.Err = true
		t.err.add(e)
		return e.Seq
	}
	t.std.add(e)
	return e.Seq
}

func (t *TaskLog) Snapshot(processID string, maxPerSection int) api.LogSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	errEntries := t.err.entries()
	stdEntries := t.std.entries()
	if maxPerSection > 0 {
		errEntries = trimLastEntries(errEntries, maxPerSection)
		stdEntries = trimLastEntries(stdEntries, maxPerSection)
	}
	return api.LogSnapshot{
		ProcessID:     processID,
		TotalSeen:     t.total,
		LineMaxBytes:  MaxStoredLineBytes,
		ErrorWarning:  api.LogBuffer{Capacity: t.err.cap(), Kept: t.err.len(), Entries: apiEntries(errEntries, "errorWarning")},
		StandardOther: api.LogBuffer{Capacity: t.std.cap(), Kept: t.std.len(), Entries: apiEntries(stdEntries, "standardOther")},
	}
}

func (t *TaskLog) Render(maxPerSection int) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	errEntries := t.err.entries()
	stdEntries := t.std.entries()
	requested := maxPerSection
	if maxPerSection > 0 {
		errEntries = trimLastEntries(errEntries, maxPerSection)
		stdEntries = trimLastEntries(stdEntries, maxPerSection)
	}

	var b strings.Builder
	b.WriteString("# memory-only log buffers\n")
	b.WriteString(fmt.Sprintf("# error_warning: kept %d/%d lines\n", t.err.len(), t.err.cap()))
	b.WriteString(fmt.Sprintf("# standard_other: kept %d/%d lines\n", t.std.len(), t.std.cap()))
	b.WriteString(fmt.Sprintf("# total_seen_lines: %d\n", t.total))
	b.WriteString(fmt.Sprintf("# line_max_bytes: %d\n", MaxStoredLineBytes))
	if requested > 0 {
		b.WriteString(fmt.Sprintf("# requested_lines_per_section: %d\n", requested))
	}
	b.WriteString(fmt.Sprintf("# displayed_now: error_warning=%d standard_other=%d total=%d\n", len(errEntries), len(stdEntries), len(errEntries)+len(stdEntries)))

	b.WriteString("\n[error_warning]\n")
	for _, e := range errEntries {
		b.WriteString(fmt.Sprintf("E#%d %s\n", e.Seq, e.Text))
	}

	b.WriteString("\n[standard_other]\n")
	for _, e := range stdEntries {
		b.WriteString(fmt.Sprintf("S#%d %s\n", e.Seq, e.Text))
	}

	return strings.TrimRight(b.String(), "\n")
}

func apiEntries(entries []Entry, category string) []api.LogEntry {
	out := make([]api.LogEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, api.LogEntry{
			Sequence:  entry.Seq,
			Timestamp: entry.Timestamp,
			Source:    entry.Source,
			Category:  category,
			Text:      entry.Text,
		})
	}
	return out
}

func trimLastEntries(in []Entry, n int) []Entry {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[len(in)-n:]
}

func isErrWarnLine(line string) bool {
	v := strings.ToLower(line)
	return strings.Contains(v, "error") ||
		strings.Contains(v, "warn") ||
		strings.Contains(v, "fatal") ||
		strings.Contains(v, "panic")
}

func truncateStoredLine(line string) string {
	if len(line) <= MaxStoredLineBytes {
		return line
	}
	suffix := fmt.Sprintf(" ... [truncated %d bytes]", len(line)-MaxStoredLineBytes)
	prefixLen := MaxStoredLineBytes
	if len(suffix) < MaxStoredLineBytes {
		prefixLen = MaxStoredLineBytes - len(suffix)
	}
	if prefixLen < 0 {
		prefixLen = 0
	}
	prefix := line[:prefixLen]
	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + suffix
}
