package lidd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"agent-coffee/internal/config"
	"agent-coffee/internal/fsutil"
	"agent-coffee/internal/ipc"
)

const lidStateDir = "/var/db/agent-coffee"

type lidManager struct {
	mu       sync.Mutex
	retry    chan struct{}
	draining bool
	active   int
	journal  string
	power    sleepBackend
}

func (l *lidManager) verify(want bool) error {
	state, err := l.power.read()
	if err != nil {
		return err
	}
	if state != want {
		return errors.New("SleepDisabled readback differs from requested value")
	}
	return nil
}

// Called under the manager lock. Only the transition from enabled sleep is journaled.
func (l *lidManager) recover() error {
	if l.active > 0 {
		return nil
	}
	b, err := os.ReadFile(l.journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(b) != "v1 prior=0 requested=1\n" {
		return errors.New("invalid lid recovery record")
	}
	state, err := l.power.read()
	if err != nil {
		return err
	}
	if state {
		if err = l.power.write(false); err != nil {
			return err
		}
		if err = l.verify(false); err != nil {
			return err
		}
	}
	if err = os.Remove(l.journal); err != nil {
		return err
	}
	return fsutil.SyncDir(filepath.Dir(l.journal))
}

func (l *lidManager) acquire() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.draining {
		return errors.New("lid helper is draining")
	}
	if l.active > 0 {
		if err := l.verify(true); err != nil {
			return err
		}
		l.active++
		return nil
	}
	if err := l.recover(); err != nil {
		return err
	}
	prior, err := l.power.read()
	if err != nil {
		return err
	}
	if !prior {
		if err = fsutil.WriteAtomic(l.journal, []byte("v1 prior=0 requested=1\n")); err != nil {
			return err
		}
		if err = l.power.write(true); err == nil {
			err = l.verify(true)
		}
		if err != nil {
			return errors.Join(err, l.recover())
		}
	}
	l.active++
	return nil
}

func (l *lidManager) release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	return l.recover()
}

// The helper stays asleep between lease events unless a restoration failed.
func (l *lidManager) scheduleRecovery() {
	select {
	case l.retry <- struct{}{}:
	default:
	}
}
func (l *lidManager) retryRecovery() {
	for range l.retry {
		for delay := time.Second; ; delay = min(2*delay, time.Minute) {
			l.mu.Lock()
			err := l.recover()
			l.mu.Unlock()
			if err == nil {
				break
			}
			fmt.Fprintln(os.Stderr, "lid recovery:", err)
			time.Sleep(delay)
		}
	}
}

func (l *lidManager) handle(c net.Conn, allowed int) {
	uid, err := peerUID(c)
	if err != nil || (uid != allowed && uid != 0) {
		ipc.Answer(c, errors.New("unauthorized lid client"))
		return
	}
	l.handleAuthorized(c, uid)
}

func (l *lidManager) handleAuthorized(c net.Conn, uid int) {
	var err error
	var m ipc.Message
	if err := ipc.Receive(c, &m); err != nil {
		return
	}
	switch m.Op {
	case "drain":
		if uid != 0 {
			ipc.Answer(c, errors.New("administrator required"))
			return
		}
		l.mu.Lock()
		if l.active > 0 {
			err = errors.New("active lid leases; finish the turns and retry uninstall")
		} else if err = l.recover(); err == nil {
			l.draining = true
		}
		l.mu.Unlock()
		ipc.Answer(c, err)
	case "check":
		l.mu.Lock()
		err = l.recover()
		if l.draining {
			err = errors.New("lid helper is draining")
		}
		if err == nil {
			_, err = l.power.read()
		}
		l.mu.Unlock()
		ipc.Answer(c, err)
	case "acquire":
		if err := config.ValidateDuration(time.Until(m.Deadline)); err != nil {
			ipc.Answer(c, err)
			return
		}
		if err := l.acquire(); err != nil {
			l.scheduleRecovery()
			ipc.Answer(c, err)
			return
		}
		defer func() {
			if err := l.release(); err != nil {
				fmt.Fprintln(os.Stderr, "lid restoration:", err)
				l.scheduleRecovery()
			}
		}()
		if time.Until(m.Deadline) <= 0 {
			ipc.Answer(c, errors.New("lease expired during acquisition"))
			return
		}
		if ipc.Answer(c, nil) != nil {
			return
		}
		c.SetReadDeadline(m.Deadline)
		var b [1]byte
		c.Read(b[:]) // EOF, protocol violation, or deadline releases this lease.
	default:
		ipc.Answer(c, errors.New("unknown lid operation"))
	}
}

func Run(uid int) error {
	if os.Geteuid() != 0 || uid <= 0 {
		return errors.New("lidd needs root and a positive authorized user ID")
	}
	if err := os.MkdirAll(lidStateDir, 0700); err != nil {
		return err
	}
	lock, err := fsutil.Lock(filepath.Join(lidStateDir, "lidd.lock"), true)
	if err != nil {
		return err
	}
	defer lock.Close()
	l := &lidManager{journal: filepath.Join(lidStateDir, "restore"), power: macSleep{}, retry: make(chan struct{}, 1)}
	if err := l.recover(); err != nil {
		fmt.Fprintln(os.Stderr, "lid startup recovery:", err)
		l.scheduleRecovery()
	}
	go l.retryRecovery()
	return ipc.Serve(lidSocket, uid, func(c net.Conn) { l.handle(c, uid) })
}
