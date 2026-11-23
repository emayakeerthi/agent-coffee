package acd

import (
	"testing"
	"time"

	"agent-coffee/internal/config"
	"agent-coffee/internal/ipc"
)

func TestStopIsScopedToTurn(t *testing.T) {
	a := controller{}
	s := agentSession{settings: config.Settings{MaxAwake: time.Minute}, turns: make(map[turnKey]*hold)}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, turn := range []string{"a", "b"} {
		if err := a.event(&s, ipc.HookEvent{Event: "UserPromptSubmit", Session: "session", Turn: turn}); err != nil {
			t.Fatal(err)
		}
	}
	defer s.release(turnKey{"session", "b"}, "cleanup")
	if err := a.event(&s, ipc.HookEvent{Event: "Stop", Session: "session", Turn: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := a.event(&s, ipc.HookEvent{Event: "UserPromptSubmit", Session: "session", Turn: "a"}); err != nil {
		t.Fatal(err)
	}
	if s.turns[turnKey{"session", "a"}] != nil || s.turns[turnKey{"session", "b"}] == nil {
		t.Fatal("stale start or unrelated stop disturbed hold state")
	}
}
