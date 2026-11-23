package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"agent-coffee/internal/acd"
	"agent-coffee/internal/config"
	"agent-coffee/internal/ipc"
	"agent-coffee/internal/lidd"
)

func Valid(s string) bool { return s == "codex" || s == "claude" }

func Launch(provider string) error {
	bin, err := exec.LookPath(provider)
	if err != nil {
		return err
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path, provider)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if cfg.LidClosed {
		if err := lidd.Check(); err != nil {
			return fmt.Errorf("lid_closed requires the installed lid helper: %w", err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args, err := providerArgs(provider, self)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(nonce[:])
	conn, err := acd.Connect(provider, self)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = ipc.Exchange(conn, ipc.Message{Op: "open", ID: id, Settings: cfg}); err != nil {
		return err
	}
	conn.SetReadDeadline(time.Time{})
	go func() {
		_, err := io.Copy(os.Stderr, conn)
		if err == nil {
			fmt.Fprintln(os.Stderr, "agent-coffee: harness daemon disconnected; sleep protection ended")
		}
	}()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "AGENT_COFFEE_SESSION="+id, "AGENT_COFFEE_PROVIDER="+provider)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case err := <-done:
			return err
		case sig := <-signals:
			if sig != os.Interrupt {
				conn.Close()
				_ = cmd.Process.Signal(sig)
			}
		}
	}
}
