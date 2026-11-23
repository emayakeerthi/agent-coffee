package acd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"agent-coffee/internal/fsutil"
)

func SocketPath(provider string) (string, error) {
	// A short, stable path works across terminals and respects sockaddr_un length.
	dir := filepath.Join("/tmp", fmt.Sprintf("agent-coffee-%d", os.Getuid()))
	err := os.Mkdir(dir, 0700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return "", errors.New("unsafe agent-coffee runtime directory")
	}
	return filepath.Join(dir, provider+".sock"), nil
}

func Connect(provider, self string) (net.Conn, error) {
	path, err := SocketPath(provider)
	if err != nil {
		return nil, err
	}
	if c, e := net.DialTimeout("unix", path, time.Second); e == nil {
		return c, nil
	}
	lock, err := fsutil.Lock(path+".start", false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if c, e := net.DialTimeout("unix", path, time.Second); e == nil {
		return c, nil
	}
	cmd := exec.Command(self, "acd", provider)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The controller needs no inherited agent credentials.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if conn, e := net.DialTimeout("unix", path, 100*time.Millisecond); e == nil {
			return conn, nil
		}
		select {
		case err := <-exited:
			return nil, fmt.Errorf("acd %s exited during startup: %v", provider, err)
		case <-time.After(25 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("acd %s did not start at %s", provider, path)
}
