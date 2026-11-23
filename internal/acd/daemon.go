package acd

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"agent-coffee/internal/config"
	"agent-coffee/internal/fsutil"
	"agent-coffee/internal/ipc"
	"agent-coffee/internal/lidd"
)

type turnKey struct{ session, turn string }

type hold struct {
	cmd   *exec.Cmd
	lid   net.Conn
	timer *time.Timer
}

type agentSession struct {
	settings config.Settings
	turns    map[turnKey]*hold
	owner    net.Conn
}

type controller struct {
	mu       sync.Mutex
	sessions map[string]*agentSession
}

func Run(provider string) error {
	path, err := SocketPath(provider)
	if err != nil {
		return err
	}
	lock, err := fsutil.Lock(path+".lock", true)
	if err != nil {
		return err
	}
	defer lock.Close()
	a := controller{sessions: make(map[string]*agentSession)}
	return ipc.Serve(path, -1, a.handle)
}

func (a *controller) handle(c net.Conn) {
	var m ipc.Message
	if err := ipc.Receive(c, &m); err != nil {
		return
	}
	if len(m.ID) != 32 {
		ipc.Answer(c, errors.New("invalid launcher ID"))
		return
	}
	a.mu.Lock()
	switch m.Op {
	case "open":
		if err := config.ValidateDuration(m.Settings.MaxAwake); err != nil {
			a.mu.Unlock()
			ipc.Answer(c, err)
			return
		}
		if _, ok := a.sessions[m.ID]; ok {
			a.mu.Unlock()
			ipc.Answer(c, errors.New("session already open"))
			return
		}
		s := &agentSession{settings: m.Settings, turns: make(map[turnKey]*hold), owner: c}
		a.sessions[m.ID] = s
		a.mu.Unlock()
		if ipc.Answer(c, nil) == nil {
			ipc.AwaitClose(c)
		}
		a.mu.Lock()
		delete(a.sessions, m.ID)
		for k := range s.turns {
			s.release(k, "launcher disconnected")
		}
		a.mu.Unlock()
	case "event":
		s := a.sessions[m.ID]
		var err error
		if s == nil {
			err = errors.New("managed session ended")
		} else {
			err = a.event(s, m.Event)
		}
		a.mu.Unlock()
		ipc.Answer(c, err)
	default:
		a.mu.Unlock()
		ipc.Answer(c, errors.New("unknown request"))
	}
}

func (s *agentSession) release(k turnKey, reason string) {
	h := s.turns[k]
	if h != nil {
		slog.Info("Releasing sleep hold", "reason", reason)
		if h.timer != nil {
			h.timer.Stop()
		}
		if h.lid != nil {
			h.lid.Close()
		}
		if h.cmd != nil {
			_ = h.cmd.Process.Kill()
		}
	}
	// Codex supplies turn IDs. Remember a terminal event so a duplicate cannot renew the deadline.
	if k.turn != "" {
		s.turns[k] = nil
	} else {
		delete(s.turns, k)
	}
}

func (a *controller) event(s *agentSession, e ipc.HookEvent) error {
	if e.Session == "" {
		return errors.New("missing provider session")
	}
	k := turnKey{e.Session, e.Turn}
	switch e.Event {
	case "SessionEnd":
		for key := range s.turns {
			if key.session == e.Session {
				s.release(key, "session ended")
				delete(s.turns, key)
			}
		}
		return nil
	case "Stop", "Interrupt", "StopFailure":
		s.release(k, e.Event)
		return nil
	case "UserPromptSubmit":
	default:
		return nil
	}
	if _, ok := s.turns[k]; ok {
		return nil
	}
	duration := s.settings.MaxAwake
	deadline := time.Now().Add(duration)
	seconds := int64((duration + time.Second - 1) / time.Second)
	args := []string{"-i", "-t", strconv.FormatInt(seconds, 10), "-w", strconv.Itoa(os.Getpid())}
	if s.settings.KeepDisplayOn {
		args = append(args, "-d")
	}
	cmd := exec.Command("/usr/bin/caffeinate", args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	h := &hold{cmd: cmd}
	s.turns[k] = h
	slog.Info("Started idle sleep hold", "duration", duration.String(), "display", s.settings.KeepDisplayOn, "caffeinate_pid", cmd.Process.Pid)
	h.timer = time.AfterFunc(duration, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if s.turns[k] == h {
			s.release(k, "deadline")
			if k.turn == "" {
				s.turns[k] = nil
			}
		}
	})
	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		defer a.mu.Unlock()
		if s.turns[k] == h {
			h.cmd = nil
			if time.Until(deadline) > time.Second {
				notifyOwner(s, fmt.Sprintf("idle assertion exited early (%v)", err))
			}
		}
	}()
	if s.settings.LidClosed {
		c, err := lidd.Acquire(deadline)
		if err != nil {
			notifyOwner(s, "lid protection unavailable: "+err.Error())
			return err
		}
		h.lid = c
		slog.Info("Acquired lid hold")
		go func() {
			ipc.AwaitClose(c)
			a.mu.Lock()
			defer a.mu.Unlock()
			if s.turns[k] == h && time.Now().Before(deadline) {
				notifyOwner(s, "lid helper disconnected")
			}
		}()
	}
	return nil
}

func notifyOwner(s *agentSession, msg string) {
	slog.Warn("Sleep protection degraded", "detail", msg)
	s.owner.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
	fmt.Fprintln(s.owner, msg)
}
