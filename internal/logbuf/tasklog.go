package logbuf

import (
	"fmt"
	"math"
	"strings"
	"sync"
)

type Entry struct {
	Seq  int64
	Text string
	Err  bool
}

const maxStoredLineBytes = 4096

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

func (t *TaskLog) Add(line string, stderr bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.total == math.MaxInt64 {
		// Keep sequence labels positive if the process runs for an extreme duration.
		t.total = 0
	}
	t.total++
	raw := line
	e := Entry{Seq: t.total, Text: truncateStoredLine(raw)}
	if stderr || isErrWarnLine(raw) {
		e.Err = true
		t.err.add(e)
		return
	}
	t.std.add(e)
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
	b.WriteString(fmt.Sprintf("# line_max_bytes: %d\n", maxStoredLineBytes))
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
	if len(line) <= maxStoredLineBytes {
		return line
	}
	suffix := fmt.Sprintf(" ... [truncated %d bytes]", len(line)-maxStoredLineBytes)
	prefixLen := maxStoredLineBytes
	if len(suffix) < maxStoredLineBytes {
		prefixLen = maxStoredLineBytes - len(suffix)
	}
	if prefixLen < 0 {
		prefixLen = 0
	}
	return line[:prefixLen] + suffix
}
