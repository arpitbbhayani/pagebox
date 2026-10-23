package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandHome("~/pagebox"); got != filepath.Join(home, "pagebox") {
		t.Fatalf("got %q", got)
	}
	if got := expandHome("/abs/x"); got != "/abs/x" {
		t.Fatalf("got %q", got)
	}
}
