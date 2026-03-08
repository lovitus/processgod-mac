package logbuf

import (
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
}
