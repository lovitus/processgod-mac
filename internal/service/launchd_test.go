package service

import (
	"strings"
	"testing"
)

func TestRenderPlistIncludesDaemonArgs(t *testing.T) {
	plist := renderPlist("/tmp/processgod-mac", "/tmp")
	checks := []string{
		"<string>/tmp/processgod-mac</string>",
		"<string>daemon</string>",
		"<string>/tmp</string>",
		"<string>com.lovitus.processgod.mac</string>",
	}
	for _, c := range checks {
		if !strings.Contains(plist, c) {
			t.Fatalf("plist missing %q", c)
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
