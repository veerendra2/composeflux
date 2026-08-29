// Package dockercompose redirects Docker Compose SDK logs (logrus) to slog used by composeflux.
package dockercompose

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// slogWriter buffers and writes complete lines to slog.
type slogWriter struct {
	mu      sync.Mutex
	level   slog.Level
	buf     bytes.Buffer
	maxSize int // Maximum buffer size in bytes
	logHook *slogHook
}

// Write buffers Docker output until complete lines can be forwarded to slog.
func (w *slogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Protect against unbounded buffer growth
	if w.buf.Len() > w.maxSize {
		w.buf.Reset()
		slog.Warn("Docker log buffer overflow, partial data dropped", "level", w.level)
	}

	w.buf.Write(p)

	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// Only keep partial line if it's reasonable size
			if len(line) < 4096 {
				w.buf.WriteString(line)
			} else {
				// Drop oversized partial log line
				slog.Debug("Dropping oversized partial log line", "size", len(line))
			}
			break
		}
		if msg := strings.TrimSpace(line); msg != "" {
			attrs := []any{"source", "docker-sdk"}
			if name := w.logHook.getStackName(); name != "" {
				attrs = append(attrs, "stack_name", name)
			}
			slog.Log(context.TODO(), w.level, msg, attrs...)
		}
	}

	// Re-allocate buffer if capacity has expanded beyond 64KB to avoid holding large 1MB arrays in heap memory
	if w.buf.Len() == 0 && w.buf.Cap() > 64*1024 {
		w.buf = bytes.Buffer{}
	}

	return len(p), nil
}

// slogHook forwards logrus logs to slog.
type slogHook struct {
	mu        sync.RWMutex
	stackName string
}

// setStackName sets the stack attribute attached to forwarded Docker logs.
func (h *slogHook) setStackName(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stackName = name
}

// getStackName safely returns the current Docker log stack attribute.
func (h *slogHook) getStackName() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stackName
}

// Levels enables forwarding for every logrus level emitted by the Docker SDK.
func (h *slogHook) Levels() []logrus.Level { return logrus.AllLevels }

// Fire translates a logrus entry into the corresponding structured slog record.
func (h *slogHook) Fire(entry *logrus.Entry) error {
	level := slog.LevelInfo
	if entry.Level <= logrus.ErrorLevel {
		level = slog.LevelError
	} else if entry.Level == logrus.WarnLevel {
		level = slog.LevelWarn
	}

	if name := h.getStackName(); name != "" {
		slog.Log(context.TODO(), level, entry.Message, "stack_name", name)
	} else {
		slog.Log(context.TODO(), level, entry.Message)
	}
	return nil
}
