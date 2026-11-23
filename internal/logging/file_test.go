package logging

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestRunAppendsStructuredEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, provider := range []string{"claude", "codex"} {
		if err := Run("acd", func() error { slog.Info("Turn ended"); return nil }, "harness", provider); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(filepath.Join(home, ".agent-coffee", "log", "agent-coffee.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("log permissions = %o", info.Mode().Perm())
	}
	decoder := json.NewDecoder(f)
	for _, provider := range []string{"claude", "codex"} {
		for _, message := range []string{"Started", "Turn ended", "Finished"} {
			var rec map[string]any
			if err := decoder.Decode(&rec); err != nil {
				t.Fatal(err)
			}
			if rec["msg"] != message || rec["harness"] != provider || rec["component"] != "acd" {
				t.Fatalf("unexpected record: %v", rec)
			}
		}
	}
}
