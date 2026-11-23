package config

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-coffee/internal/fsutil"
	"go.yaml.in/yaml/v3"
)

type Settings struct {
	LidClosed     bool
	KeepDisplayOn bool
	MaxAwake      time.Duration
}

type overrides struct {
	Lid     *bool   `yaml:"lid_closed"`
	Display *bool   `yaml:"keep_display_on"`
	Limit   *string `yaml:"max_awake"`
}

const defaultConfig = "defaults:\n  lid_closed: false\n  keep_display_on: false\n  max_awake: \"90m\"\n"

func Path() (string, error) {
	base, err := fsutil.UserDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, "config", "config.yml")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return path, err
	}

	oldPath := filepath.Join(filepath.Dir(base), ".config", "agent-coffee", "config.yml")
	data, err := os.ReadFile(oldPath)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	// Publish the complete copy only if another launcher or editor hasn't created it.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return "", err
	}
	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return path, nil
}

func Load(path, provider string) (Settings, error) {
	cfg := Settings{MaxAwake: 90 * time.Minute}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	var doc struct {
		Defaults overrides
		Claude   overrides
		Codex    overrides
	}
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if err := d.Decode(&doc); err != nil && err != io.EOF {
		return cfg, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return cfg, errors.New("expected a single YAML document")
	}
	specific := doc.Codex
	if provider == "claude" {
		specific = doc.Claude
	}
	for _, v := range []overrides{doc.Defaults, specific} {
		if v.Lid != nil {
			cfg.LidClosed = *v.Lid
		}
		if v.Display != nil {
			cfg.KeepDisplayOn = *v.Display
		}
		if v.Limit != nil {
			cfg.MaxAwake, err = time.ParseDuration(*v.Limit)
			if err != nil {
				return cfg, err
			}
		}
	}
	return cfg, ValidateDuration(cfg.MaxAwake)
}

func ValidateDuration(d time.Duration) error {
	if d <= 0 || d > 2147483647*time.Second {
		return errors.New("max_awake must be positive and fit caffeinate's seconds argument")
	}
	return nil
}

func Edit() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, err = f.WriteString(defaultConfig)
		cerr := f.Close()
		if err != nil {
			return err
		}
		if cerr != nil {
			return cerr
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	args := strings.Fields(editor)
	if len(args) == 0 {
		return errors.New("empty editor")
	}
	cmd := exec.Command(args[0], append(args[1:], p)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
