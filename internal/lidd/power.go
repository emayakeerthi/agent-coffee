package lidd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

type sleepBackend interface {
	read() (bool, error)
	write(bool) error
}

type macSleep struct{}

var sleepLine = regexp.MustCompile(`(?m)^\s*[| ]*"SleepDisabled"\s*=\s*(Yes|No)\s*$`)

func (macSleep) read() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPMrootDomain").Output()
	if err != nil {
		return false, err
	}
	all := sleepLine.FindAllSubmatch(out, 2)
	if len(all) != 1 {
		return false, errors.New("ambiguous IOPMrootDomain SleepDisabled value")
	}
	return string(all[0][1]) == "Yes", nil
}

func (macSleep) write(disabled bool) error {
	val := "0"
	if disabled {
		val = "1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/pmset", "disablesleep", val).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pmset: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
