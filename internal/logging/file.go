package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"agent-coffee/internal/fsutil"
)

func openFile() (*os.File, error) {
	base, err := fsutil.UserDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, "log")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "agent-coffee.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}

// Run routes component and lifecycle events to the same append-only JSON log.
func Run(component string, run func() error, attrs ...any) error {
	file, err := openFile()
	if err != nil {
		slog.Warn("Could not open Agent Coffee log", "error", err)
		return run()
	}
	defer file.Close()
	previous := slog.Default()
	logger := slog.New(slog.NewJSONHandler(file, nil)).With("component", component, "pid", os.Getpid()).With(attrs...)
	slog.SetDefault(logger)
	defer slog.SetDefault(previous)
	logger.Info("Started")
	if err := run(); err != nil {
		logger.Error("Exited with an error", "error_type", fmt.Sprintf("%T", err))
		return err
	}
	logger.Info("Finished")
	return nil
}
