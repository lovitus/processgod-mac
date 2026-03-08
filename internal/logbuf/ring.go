package logbuf

import (
	"strings"
	"sync"
)

// Ring stores only the most recent N log lines in memory.
type Ring struct {
	mu       sync.Mutex
	lines    []string
	capacity int
	head     int
	count    int
}

func New(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Ring{
		lines:    make([]string, capacity),
		capacity: capacity,
	}
}

func (r *Ring) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines[r.head] = line
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
}

func (r *Ring) Last(n int) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.count == 0 {
		return ""
	}
	if n <= 0 || n > r.count {
		n = r.count
	}
	start := (r.head - n + r.capacity) % r.capacity

	var b strings.Builder
	for i := 0; i < n; i++ {
		idx := (start + i) % r.capacity
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(r.lines[idx])
	}
	return b.String()
}

func (r *Ring) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.lines {
		r.lines[i] = ""
	}
	r.head = 0
	r.count = 0
}
