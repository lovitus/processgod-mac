package logbuf

import (
	"math"
	"strconv"
	"strings"
	"testing"
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
	long := strings.Repeat("a", maxStoredLineBytes+500) + "error-end"

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
	if len(storedLine) > maxStoredLineBytes {
		t.Fatalf("stored line exceeds max bytes: got=%d want<=%d", len(storedLine), maxStoredLineBytes)
	}
}
