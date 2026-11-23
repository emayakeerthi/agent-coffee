package probe

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

const heartbeatInterval = 2 * time.Second

func Run() {
	mode := flag.String("mode", "baseline", "baseline or idle")
	duration := flag.Duration("duration", 5*time.Minute, "wall-clock runtime (at least 5s)")
	flag.Parse()
	log.SetFlags(0)
	if runtime.GOOS != "darwin" {
		log.Fatal("the sleep probe requires macOS")
	}
	if flag.NArg() != 0 || (*mode != "baseline" && *mode != "idle") || *duration < 5*time.Second {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runProbe(ctx, *mode, *duration); err != nil {
		log.Fatal(err)
	}
}

func runProbe(ctx context.Context, mode string, duration time.Duration) error {
	file, err := os.CreateTemp("", "agent-coffee-*.log")
	if err != nil {
		return err
	}
	defer file.Close()
	fmt.Println(file.Name())
	if _, err := fmt.Fprintf(file, "mode=%s duration=%s interval=%s\n", mode, duration, heartbeatInterval); err != nil {
		return err
	}

	snapshot(ctx, file, "batt")
	snapshot(ctx, file, "custom")
	snapshot(ctx, file, "assertions")
	if ctx.Err() != nil {
		return nil
	}

	var assertionDone <-chan error
	if mode == "idle" {
		assertionCtx, release := context.WithCancel(ctx)
		// Give the probe time to write its final heartbeat before the assertion expires.
		seconds := int64(duration/time.Second) + 3
		cmd := exec.CommandContext(assertionCtx, "/usr/bin/caffeinate", "-i", "-t", strconv.FormatInt(seconds, 10))
		cmd.Stderr = file
		if err := cmd.Start(); err != nil {
			release()
			return fmt.Errorf("start caffeinate: %w", err)
		}
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
			close(done)
		}()
		assertionDone = done
		defer func() {
			release()
			<-done
		}()
	}

	// Strip the monotonic clock so a gap includes time spent asleep.
	started := time.Now().Round(0)
	deadline := started.Add(duration)
	previous := started
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	timer := time.NewTimer(duration)
	defer timer.Stop()

	assertionsPending := mode == "idle"
	for {
		now := time.Now().Round(0)
		if _, err := fmt.Fprintf(file, "%s heartbeat gap=%.3fs\n", now.Format(time.RFC3339Nano), now.Sub(previous).Seconds()); err != nil {
			return err
		}
		if !now.Before(deadline) {
			_, err := fmt.Fprintf(file, "%s finished\n", now.Format(time.RFC3339Nano))
			return err
		}
		previous = now
		select {
		case <-ctx.Done():
			_, err := fmt.Fprintln(file, "interrupted")
			return err
		case err := <-assertionDone:
			if ctx.Err() != nil {
				return nil
			}
			if time.Now().Round(0).Before(deadline) {
				return fmt.Errorf("caffeinate exited before the probe ended (wait result: %v)", err)
			}
		case <-ticker.C:
			if assertionsPending {
				snapshot(ctx, file, "assertions")
				assertionsPending = false
			}
		case <-timer.C:
		}
	}
}

func snapshot(ctx context.Context, file *os.File, query string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	fmt.Fprintf(file, "\n%s pmset -g %s\n", time.Now().Format(time.RFC3339Nano), query)
	cmd := exec.CommandContext(ctx, "/usr/bin/pmset", "-g", query)
	cmd.Stdout = file
	cmd.Stderr = file
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(file, "pmset failed: %v\n", err)
	}
	fmt.Fprintln(file)
}
