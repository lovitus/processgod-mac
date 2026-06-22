package service

import (
	"strings"
	"testing"
)

func TestRenderPlistIncludesDaemonArgs(t *testing.T) {
	plist := renderPlist("/tmp/processgod-mac", "/tmp", false)
	checks := []string{
		"<string>/tmp/processgod-mac</string>",
		"<string>daemon</string>",
		"<string>--scope</string>",
		"<string>user</string>",
		"<string>/tmp</string>",
		"<string>com.lovitus.processgod.mac</string>",
		"<key>PROCESSGOD_HOME</key>",
	}
	for _, c := range checks {
		if !strings.Contains(plist, c) {
			t.Fatalf("plist missing %q", c)
		}
	}
}

func TestRenderSystemPlistUsesSystemScope(t *testing.T) {
	plist := renderPlist("/tmp/processgod-mac", "/Library/Application Support/ProcessGodMac", true)
	checks := []string{
		"<string>daemon</string>",
		"<string>--scope</string>",
		"<string>system</string>",
		"<string>/Library/Application Support/ProcessGodMac</string>",
	}
	for _, c := range checks {
		if !strings.Contains(plist, c) {
			t.Fatalf("system plist missing %q", c)
		}
	}
}

func TestXmlEscape(t *testing.T) {
	in := `a&b<c>"d"'e'`
	out := xmlEscape(in)
	want := "a&amp;b&lt;c&gt;&quot;d&quot;&apos;e&apos;"
	if out != want {
		t.Fatalf("xml escape mismatch: want %q got %q", want, out)
	}
}
