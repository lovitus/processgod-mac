package logbuf

import "testing"

func TestRingKeepsLatestLines(t *testing.T) {
	r := New(3)
	r.Add("l1")
	r.Add("l2")
	r.Add("l3")
	r.Add("l4")

	got := r.Last(3)
	want := "l2\nl3\nl4"
	if got != want {
		t.Fatalf("last lines mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestRingLastZeroUsesAll(t *testing.T) {
	r := New(5)
	r.Add("a")
	r.Add("b")
	if got := r.Last(0); got != "a\nb" {
		t.Fatalf("unexpected output: %q", got)
	}
}
