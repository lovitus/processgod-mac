package logbuf

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Entry struct {
	Seq  int64
	Text string
	Err  bool
}

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
	t.total++
	e := Entry{Seq: t.total, Text: line}
	if stderr || isErrWarnLine(line) {
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
	if maxPerSection > 0 {
		errEntries = trimLastEntries(errEntries, maxPerSection)
		stdEntries = trimLastEntries(stdEntries, maxPerSection)
	}

	var b strings.Builder
	b.WriteString("# memory-only log buffers\n")
	b.WriteString(fmt.Sprintf("# error_warning: kept %d/%d lines\n", t.err.len(), t.err.cap()))
	b.WriteString(fmt.Sprintf("# standard_other: kept %d/%d lines\n", t.std.len(), t.std.cap()))
	b.WriteString(fmt.Sprintf("# total_seen_lines: %d\n", t.total))

	b.WriteString("\n[error_warning]\n")
	for _, e := range errEntries {
		b.WriteString(fmt.Sprintf("E#%d %s\n", e.Seq, e.Text))
	}

	b.WriteString("\n[standard_other]\n")
	for _, e := range stdEntries {
		b.WriteString(fmt.Sprintf("S#%d %s\n", e.Seq, e.Text))
	}

	b.WriteString("\n[merged_recent]\n")
	merged := append([]Entry{}, errEntries...)
	merged = append(merged, stdEntries...)
	sort.Slice(merged, func(i, j int) bool { return merged[i].Seq < merged[j].Seq })
	for _, e := range merged {
		prefix := "S"
		if e.Err {
			prefix = "E"
		}
		b.WriteString(fmt.Sprintf("%s#%d %s\n", prefix, e.Seq, e.Text))
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
