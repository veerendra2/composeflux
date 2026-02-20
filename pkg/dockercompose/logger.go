// Package dockercompose redirects Docker Compose SDK logs (logrus) to slog used by composeflux.
package dockercompose

import (
	"bytes"
	"context"
	"log/slog"
	"strings"

	"github.com/sirupsen/logrus"
)

// slogWriter buffers and writes complete lines to slog.
type slogWriter struct {
	level   slog.Level
	buf     bytes.Buffer
	maxSize int // Maximum buffer size in bytes
}

func (w *slogWriter) Write(p []byte) (int, error) {
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
				// Drop oversized partial line
				slog.Warn("Dropping oversized partial log line", "size", len(line))
			}
			break
		}
		if msg := strings.TrimSpace(line); msg != "" {
			slog.Log(context.TODO(), w.level, msg, "source", "docker-sdk")
		}
	}

	return len(p), nil
}

// slogHook forwards logrus logs to slog.
type slogHook struct{}

func (h *slogHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *slogHook) Fire(entry *logrus.Entry) error {
	level := slog.LevelInfo
	if entry.Level <= logrus.ErrorLevel {
		level = slog.LevelError
	} else if entry.Level == logrus.WarnLevel {
		level = slog.LevelWarn
	}

	slog.Log(context.TODO(), level, entry.Message)
	return nil
}
