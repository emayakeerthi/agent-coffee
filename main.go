package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"agent-coffee/internal/acd"
	"agent-coffee/internal/config"
	"agent-coffee/internal/harness"
	"agent-coffee/internal/lidd"
	"agent-coffee/internal/logging"
	"agent-coffee/internal/probe"
)

func main() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "Agent Coffee requires macOS")
		os.Exit(1)
	}
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0 || args[0] == "-h" || args[0] == "--help":
		fmt.Println("Usage: agent-coffee {codex|claude|config}\nSleep experiment: agent-coffee -mode {baseline|idle} -duration 5m")
		return
	case strings.HasPrefix(args[0], "-"):
		probe.Run()
		return
	case len(args) == 1 && harness.Valid(args[0]):
		err = logging.Run("launcher", func() error { return harness.Launch(args[0]) }, "harness", args[0])
	case len(args) == 1 && args[0] == "config":
		err = logging.Run("config", config.Edit)
	case len(args) == 2 && args[0] == "hook" && harness.Valid(args[1]):
		harness.RunHook(args[1])
		return
	case len(args) == 2 && args[0] == "acd" && harness.Valid(args[1]):
		err = logging.Run("acd", func() error { return acd.Run(args[1]) }, "harness", args[1])
	case len(args) == 1 && args[0] == "drain-lidd":
		err = lidd.Drain()
	case len(args) == 2 && args[0] == "lidd":
		var uid int
		uid, err = strconv.Atoi(args[1])
		if err == nil {
			err = lidd.Run(uid)
		}
	default:
		err = errors.New("expected codex, claude, or config")
	}
	if err != nil {
		var status *exec.ExitError
		if errors.As(err, &status) {
			code := status.ExitCode()
			if code < 0 {
				code = 128 + int(status.Sys().(syscall.WaitStatus).Signal())
			}
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, "agent-coffee:", err)
		os.Exit(1)
	}
}
