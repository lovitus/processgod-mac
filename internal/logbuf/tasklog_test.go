package logbuf

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTaskLogCapacityAndRender(t *testing.T) {
	l := NewTaskLog(3, 2)

	l.Add("info1", false)
	l.Add("warn one", false)
	l.Add("info2", false)
	l.Add("info3", false)
	l.Add("error one", false)
	l.Add("stderr plain", true)

	out := l.Render(0)
	if !strings.Contains(out, "error_warning: kept 3/3") {
		t.Fatalf("missing err buffer summary: %s", out)
	}
	if !strings.Contains(out, "standard_other: kept 2/2") {
		t.Fatalf("missing std buffer summary: %s", out)
	}
	if !strings.Contains(out, "total_seen_lines: 6") {
		t.Fatalf("missing total counter: %s", out)
	}
	if !strings.Contains(out, "E#6 stderr plain") {
		t.Fatalf("expected stderr classified as error/warning section: %s", out)
	}
	if strings.Contains(out, "S#1 info1") {
		t.Fatalf("old std line should be rotated out: %s", out)
	}
	if strings.Contains(out, "[merged_recent]") {
		t.Fatalf("merged section should not be rendered: %s", out)
	}
}

func TestTaskLogSeqRolloverStaysPositive(t *testing.T) {
	l := NewTaskLog(3, 3)
	l.total = math.MaxInt64

	l.Add("after rollover", false)
	out := l.Render(0)
	if !strings.Contains(out, "S#1 after rollover") {
		t.Fatalf("expected positive sequence after rollover: %s", out)
	}
}

func TestTaskLogTruncatesStoredLineBytes(t *testing.T) {
	l := NewTaskLog(3, 3)
	long := strings.Repeat("a", MaxStoredLineBytes+500) + "error-end"

	l.Add(long, false)
	out := l.Render(0)

	if !strings.Contains(out, "[truncated ") {
		t.Fatalf("expected truncation marker: %s", out)
	}
	if !strings.Contains(out, "E#1 ") {
		t.Fatalf("expected error classification from full raw line: %s", out)
	}

	var storedLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "E#1 ") {
			storedLine = strings.TrimPrefix(ln, "E#"+strconv.Itoa(1)+" ")
			break
		}
	}
	if storedLine == "" {
		t.Fatalf("missing stored line in output: %s", out)
	}
	if len(storedLine) > MaxStoredLineBytes {
		t.Fatalf("stored line exceeds max bytes: got=%d want<=%d", len(storedLine), MaxStoredLineBytes)
	}
}

func TestTaskLogTruncationKeepsValidUTF8(t *testing.T) {
	l := NewTaskLog(1, 1)
	l.Add(strings.Repeat("界", MaxStoredLineBytes), false)
	snapshot := l.Snapshot("utf8", 0)
	text := snapshot.StandardOther.Entries[0].Text
	if !utf8.ValidString(text) || len(text) > MaxStoredLineBytes {
		t.Fatalf("invalid bounded UTF-8 log line: bytes=%d valid=%v", len(text), utf8.ValidString(text))
	}
}

func TestTaskLogStructuredSnapshot(t *testing.T) {
	l := NewTaskLog(2, 1)
	l.Add("one", false)
	l.Add("warning two", false)
	l.Add("stderr three", true)
	l.Add("four", false)

	snapshot := l.Snapshot("job", 0)
	if snapshot.ProcessID != "job" || snapshot.TotalSeen != 4 || snapshot.LineMaxBytes != MaxStoredLineBytes {
		t.Fatalf("unexpected metadata: %+v", snapshot)
	}
	if snapshot.ErrorWarning.Kept != 2 || snapshot.StandardOther.Kept != 1 {
		t.Fatalf("unexpected capacities: %+v", snapshot)
	}
	if got := snapshot.ErrorWarning.Entries[1]; got.Source != "stderr" || got.Category != "errorWarning" || got.Timestamp.IsZero() {
		t.Fatalf("unexpected structured entry: %+v", got)
	}
}
