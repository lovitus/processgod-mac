package config

import "testing"

func TestSplitArgs(t *testing.T) {
	args := SplitArgs(`--name "hello world" --path '/tmp/a b' plain`)
	want := []string{"--name", "hello world", "--path", "/tmp/a b", "plain"}

	if len(args) != len(want) {
		t.Fatalf("arg count mismatch: want %d got %d (%v)", len(want), len(args), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d]: want %q got %q", i, want[i], args[i])
		}
	}
}

func TestNormalizeFillsLegacyFields(t *testing.T) {
	it := Item{ID: "a", EXEFullPath: "/bin/echo", StartupParams: "hello world"}
	it.Normalize()
	if it.ExecPath != "/bin/echo" {
		t.Fatalf("expected exec path from legacy field, got %q", it.ExecPath)
	}
	if len(it.Args) != 2 || it.Args[0] != "hello" || it.Args[1] != "world" {
		t.Fatalf("unexpected args: %#v", it.Args)
	}
}
