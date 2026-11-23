package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
)

func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agent-coffee"), nil
}

func Lock(path string, nonblocking bool) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	flag := syscall.LOCK_EX
	if nonblocking {
		flag |= syscall.LOCK_NB
	}
	if err = syscall.Flock(int(f.Fd()), flag); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return SyncDir(dir)
}

func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
