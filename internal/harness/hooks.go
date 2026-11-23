package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-coffee/internal/acd"
	"agent-coffee/internal/fsutil"
	"agent-coffee/internal/ipc"
)

type hookGroup struct {
	Hooks []map[string]string `json:"hooks"`
}

func providerArgs(provider, self string) ([]string, error) {
	command := "'" + strings.ReplaceAll(self, "'", "'\"'\"'") + "' hook " + provider
	events := []string{"UserPromptSubmit", "Stop", "SessionEnd"}
	if provider == "claude" {
		events = append(events, "StopFailure")
	} else {
		events = append(events, "Interrupt")
	}
	group := hookGroup{Hooks: []map[string]string{{"type": "command", "command": command}}}
	if provider == "claude" {
		hooks := map[string][]hookGroup{}
		for _, name := range events {
			hooks[name] = []hookGroup{group}
		}
		b, err := json.Marshal(map[string]any{"hooks": hooks})
		return []string{"--settings", string(b)}, err
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(dir, ".codex")
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	lock, err := fsutil.Lock(filepath.Join(home, ".agent-coffee-hooks.lock"), false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	path := filepath.Join(home, "hooks.json")
	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	doc := map[string]json.RawMessage{}
	hooks := map[string][]json.RawMessage{}
	if err == nil {
		if err = json.Unmarshal(original, &doc); err != nil || doc == nil {
			return nil, fmt.Errorf("invalid Codex hook configuration in %s", path)
		}
		if raw, ok := doc["hooks"]; ok {
			if err = json.Unmarshal(raw, &hooks); err != nil || hooks == nil {
				return nil, fmt.Errorf("invalid hooks object in %s", path)
			}
		}
	}
	encoded, _ := json.Marshal(group)
	changed := false
	for _, name := range events {
		found := false
		for _, raw := range hooks[name] {
			var parsed hookGroup
			if json.Unmarshal(raw, &parsed) == nil {
				for _, h := range parsed.Hooks {
					if h["type"] == "command" && h["command"] == command {
						found = true
					}
				}
			}
		}
		if !found {
			hooks[name] = append(hooks[name], encoded)
			changed = true
		}
	}
	if changed {
		doc["hooks"], _ = json.Marshal(hooks)
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		current, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if !bytes.Equal(original, current) {
			return nil, errors.New("Codex hook configuration changed during setup; retry launch")
		}
		if err := fsutil.WriteAtomic(path, append(data, '\n')); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "Agent Coffee hooks registered. Review and trust them in Codex with /hooks.")
	}
	return []string{"--no-daemon"}, nil
}

func RunHook(provider string) {
	// Both providers accept a neutral object; hook failures must not veto the turn.
	defer fmt.Println("{}")
	id := os.Getenv("AGENT_COFFEE_SESSION")
	if id == "" || os.Getenv("AGENT_COFFEE_PROVIDER") != provider {
		return
	}
	var h ipc.HookEvent
	if json.NewDecoder(io.LimitReader(os.Stdin, 16<<20)).Decode(&h) != nil || h.Session == "" {
		return
	}
	path, err := acd.SocketPath(provider)
	if err != nil {
		return
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		defer conn.Close()
		err = ipc.Exchange(conn, ipc.Message{Op: "event", ID: id, Event: h})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-coffee power event:", err)
	}
}
