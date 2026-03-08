package cron

import (
	"testing"
	"time"
)

func TestParseAndMatch(t *testing.T) {
	s, err := Parse("*/5 1 * * 1-5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	matchTime := time.Date(2026, time.March, 9, 1, 10, 0, 0, time.Local) // Monday
	if !s.Matches(matchTime) {
		t.Fatalf("expected match at %v", matchTime)
	}

	noMatch := time.Date(2026, time.March, 9, 2, 10, 0, 0, time.Local)
	if s.Matches(noMatch) {
		t.Fatalf("expected no match at %v", noMatch)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	cases := []string{
		"",
		"* * * *",
		"61 * * * *",
		"*/0 * * * *",
		"* * * * 8",
	}
	for _, c := range cases {
		if _, err := Parse(c); err == nil {
			t.Fatalf("expected parse error for %q", c)
		}
	}
}
