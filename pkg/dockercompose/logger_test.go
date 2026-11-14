package dockercompose

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

// recordingHandler captures slog records for assertions.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var messages []string
	for _, r := range h.records {
		messages = append(messages, r.Message)
	}
	return messages
}

func recordAttrs(r slog.Record) map[string]string {
	attrs := make(map[string]string)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	return attrs
}

// captureLogs routes the default slog logger to a recorder for the current spec.
func captureLogs() *recordingHandler {
	handler := &recordingHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	DeferCleanup(slog.SetDefault, previous)
	return handler
}

var _ = Describe("slogWriter", func() {
	var (
		logs   *recordingHandler
		hook   *slogHook
		writer *slogWriter
	)

	BeforeEach(func() {
		logs = captureLogs()
		hook = &slogHook{}
		writer = &slogWriter{level: slog.LevelWarn, maxSize: 64, logHook: hook}
	})

	It("logs complete trimmed lines and skips blank lines", func() {
		n, err := writer.Write([]byte("  first  \n\nsecond\n"))
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(len("  first  \n\nsecond\n")))
		Expect(logs.messages()).To(Equal([]string{"first", "second"}))
		Expect(logs.records[0].Level).To(Equal(slog.LevelWarn))
		Expect(recordAttrs(logs.records[0])).To(Equal(map[string]string{"source": "docker-sdk"}))
	})

	It("buffers partial lines until they are completed", func() {
		_, _ = writer.Write([]byte("pulling "))
		Expect(logs.messages()).To(BeEmpty())

		_, _ = writer.Write([]byte("image\n"))
		Expect(logs.messages()).To(Equal([]string{"pulling image"}))
	})

	It("adds the current stack name", func() {
		hook.setStackName("app")
		_, _ = writer.Write([]byte("started\n"))
		Expect(recordAttrs(logs.records[0])).To(HaveKeyWithValue("stack_name", "app"))
	})

	It("drops oversized partial lines", func() {
		_, _ = writer.Write([]byte(strings.Repeat("x", 5000)))
		_, _ = writer.Write([]byte("tail\n"))
		Expect(logs.messages()).To(Equal([]string{"Dropping oversized partial log line", "tail"}))
	})
})

var _ = Describe("slogHook", func() {
	DescribeTable("Fire maps logrus levels to slog levels",
		func(level logrus.Level, expected slog.Level) {
			logs := captureLogs()
			Expect((&slogHook{}).Fire(&logrus.Entry{Level: level, Message: "msg"})).To(Succeed())
			Expect(logs.records).To(HaveLen(1))
			Expect(logs.records[0].Level).To(Equal(expected))
			Expect(recordAttrs(logs.records[0])).To(BeEmpty())
		},
		Entry("panic", logrus.PanicLevel, slog.LevelError),
		Entry("error", logrus.ErrorLevel, slog.LevelError),
		Entry("warn", logrus.WarnLevel, slog.LevelWarn),
		Entry("info", logrus.InfoLevel, slog.LevelInfo),
		Entry("debug", logrus.DebugLevel, slog.LevelInfo),
	)

	It("adds the current stack name", func() {
		logs := captureLogs()
		hook := &slogHook{}
		hook.setStackName("app")
		Expect(hook.Fire(&logrus.Entry{Level: logrus.WarnLevel, Message: "msg"})).To(Succeed())
		Expect(recordAttrs(logs.records[0])).To(Equal(map[string]string{"stack_name": "app"}))
	})
})
