package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("defaults:\n  lid_closed: true\n  max_awake: 10m\ncodex:\n  lid_closed: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"codex", "claude"} {
		v, err := Load(path, provider)
		if err != nil || v.MaxAwake != 10*time.Minute || v.LidClosed != (provider == "claude") {
			t.Fatal(v, err)
		}
	}
}

func TestPathMigratesExistingUserConfigOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".agent-coffee", "config", "config.yml")
	legacy := filepath.Join(home, ".config", "agent-coffee", "config.yml")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# keep this comment\ncodex:\n  lid_closed: true\n")
	if err := os.WriteFile(legacy, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{string(original), defaultConfig} {
		got, err := Path()
		if err != nil || got != want {
			t.Fatalf("Path() = %q, %v", got, err)
		}
		data, err := os.ReadFile(got)
		if err != nil || string(data) != content {
			t.Fatalf("config = %q, %v", data, err)
		}
		if err := os.WriteFile(want, []byte(defaultConfig), 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(legacy)
	if err != nil || string(data) != string(original) {
		t.Fatalf("legacy config = %q, %v", data, err)
	}
}
