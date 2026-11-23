package lidd

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-coffee/internal/ipc"
)

type fakeSleep struct {
	disabled    bool
	writes      []bool
	failRestore bool
}

func (f *fakeSleep) read() (bool, error) { return f.disabled, nil }

func (f *fakeSleep) write(v bool) error {
	if !v && f.failRestore {
		return errors.New("restore failed")
	}
	f.disabled = v
	f.writes = append(f.writes, v)
	return nil
}

func TestLidOwnership(t *testing.T) {
	for _, prior := range []bool{false, true} {
		f := &fakeSleep{disabled: prior}
		l := lidManager{power: f, journal: filepath.Join(t.TempDir(), "restore")}
		for i := 0; i < 2; i++ {
			if err := l.acquire(); err != nil {
				t.Fatal(err)
			}
		}
		if err := l.release(); err != nil || !f.disabled {
			t.Fatal("first release disturbed the other lease", err)
		}
		if err := l.release(); err != nil || f.disabled != prior {
			t.Fatal("last release failed to restore baseline", err)
		}
		if prior && len(f.writes) != 0 {
			t.Fatal("preexisting override was modified")
		}
	}
}

func TestLidCrashRecovery(t *testing.T) {
	f := &fakeSleep{}
	path := filepath.Join(t.TempDir(), "restore")
	first := lidManager{power: f, journal: path}
	if err := first.acquire(); err != nil {
		t.Fatal(err)
	}
	restarted := lidManager{power: f, journal: path}
	f.failRestore = true
	if err := restarted.acquire(); err == nil {
		t.Fatal("new lease accepted during failed recovery")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("recovery journal lost", err)
	}
	f.failRestore = false
	if err := restarted.recover(); err != nil || f.disabled {
		t.Fatal("recovery failed", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal still present", err)
	}
}

func TestLidLeaseConnectionAndDeadline(t *testing.T) {
	fake := &fakeSleep{}
	m := &lidManager{power: fake, journal: filepath.Join(t.TempDir(), "restore")}
	lease := func(d time.Duration) (net.Conn, <-chan struct{}) {
		t.Helper()
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); defer server.Close(); m.handleAuthorized(server, 501) }()
		if err := ipc.Exchange(client, ipc.Message{Op: "acquire", Deadline: time.Now().Add(d)}); err != nil {
			t.Fatal(err)
		}
		return client, done
	}
	a, doneA := lease(time.Minute)
	b, doneB := lease(200 * time.Millisecond)
	defer b.Close()
	a.Close()
	<-doneA
	m.mu.Lock()
	protected := m.active == 1 && fake.disabled
	m.mu.Unlock()
	if !protected {
		t.Fatal("disconnect released another harness's lease")
	}
	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		t.Fatal("helper missed deadline")
	}
	if fake.disabled || m.active != 0 {
		t.Fatal("helper did not restore on expiry")
	}
}

// Exercise the read-only hardware observation with the exact backend used for restore verification.
func TestReadSleepDisabled(t *testing.T) {
	if _, err := (macSleep{}).read(); err != nil {
		t.Fatal(err)
	}
}
