package main

import (
	"testing"

	"github.com/lovitus/processgod-mac/internal/runtimepaths"
)

func TestServiceWorkingRootUsesSystemStoreForSystemMode(t *testing.T) {
	t.Setenv("PROCESSGOD_HOME", "/tmp/processgod-user-override")
	root, err := serviceWorkingRoot(true)
	if err != nil {
		t.Fatalf("serviceWorkingRoot(system): %v", err)
	}
	if root != runtimepaths.SystemRoot {
		t.Fatalf("system service root mismatch: want %q got %q", runtimepaths.SystemRoot, root)
	}
}

func TestServiceWorkingRootKeepsUserOverrideForUserMode(t *testing.T) {
	override := "/tmp/processgod-user-override"
	t.Setenv("PROCESSGOD_HOME", override)
	root, err := serviceWorkingRoot(false)
	if err != nil {
		t.Fatalf("serviceWorkingRoot(user): %v", err)
	}
	if root != override {
		t.Fatalf("user service root mismatch: want %q got %q", override, root)
	}
}
