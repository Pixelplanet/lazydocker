package utils

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestBuildOSC52(t *testing.T) {
	// Ensure multiplexer wrapping is not triggered for the base case.
	oldTmux := os.Getenv("TMUX")
	oldTerm := os.Getenv("TERM")
	_ = os.Unsetenv("TMUX")
	_ = os.Setenv("TERM", "xterm-256color")
	defer func() {
		_ = os.Setenv("TMUX", oldTmux)
		_ = os.Setenv("TERM", oldTerm)
	}()

	payload := base64.StdEncoding.EncodeToString([]byte("hello world"))
	got := buildOSC52(payload)
	want := "\x1b]52;c;" + payload + "\x07"

	if got != want {
		t.Errorf("buildOSC52() = %q, want %q", got, want)
	}
}

func TestBuildOSC52Tmux(t *testing.T) {
	oldTmux := os.Getenv("TMUX")
	_ = os.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	defer func() { _ = os.Setenv("TMUX", oldTmux) }()

	payload := base64.StdEncoding.EncodeToString([]byte("data"))
	got := buildOSC52(payload)

	// The sequence must be wrapped in tmux's passthrough.
	if len(got) < 6 || got[:6] != "\x1bPtmux" {
		t.Errorf("buildOSC52() under tmux should be wrapped, got %q", got)
	}
}
