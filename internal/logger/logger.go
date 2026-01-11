package logger

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/kardianos/service"
	slogmulti "github.com/samber/slog-multi"
)

// Setup configures the global slog.Logger to write to both the service logger and the specified file.
func Setup(svc service.Logger, logFile io.Writer) *slog.Logger {
	// File Handler: Text format for readability in the local log file.
	fileHandler := slog.NewTextHandler(logFile, nil)

	// Service Handler: Adapts slog to kardianos/service logger.
	svcHandler := &ServiceHandler{svc: svc}

	// Fanout: Send logs to both handlers.
	fanout := slogmulti.Fanout(fileHandler, svcHandler)

	// Create Logger
	logger := slog.New(fanout)

	// Set as global default so slog.Info() works out of the box if needed.
	slog.SetDefault(logger)

	return logger
}

// ServiceHandler adapts slog.Handler to service.Logger.
// It formats the log record (message + attributes) into a string and passes it to the underlying service logger.
type ServiceHandler struct {
	svc    service.Logger
	attrs  []slog.Attr
	groups []string
}

// Enabled always returns true as the service logger's filtering is managed by the OS or the service wrapper.
func (h *ServiceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

// Handle formats the record and writes it to the service logger.
func (h *ServiceHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.svc == nil {
		return nil
	}

	var buf bytes.Buffer
	// Use a temporary TextHandler to format the record attributes into a string.
	// We strip Time and Level because the service logger (event log/syslog) usually adds these.
	th := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	})

	// Replay accumulated state (groups and attributes) onto the temporary handler.
	var handler slog.Handler = th
	for _, g := range h.groups {
		handler = handler.WithGroup(g)
	}
	handler = handler.WithAttrs(h.attrs)

	// Format the current record.
	if err := handler.Handle(ctx, r); err != nil {
		return err
	}

	// The buffer now contains the formatted log entry (e.g., "msg=... key=val ...\n").
	msg := strings.TrimSpace(buf.String())

	// Dispatch to the appropriate service logger method based on level.
	switch r.Level {
	case slog.LevelError:
		return h.svc.Error(msg)
	case slog.LevelWarn:
		return h.svc.Warning(msg)
	case slog.LevelInfo:
		return h.svc.Info(msg)
	default:
		return h.svc.Info(msg)
	}
}

// WithAttrs returns a new ServiceHandler with the given attributes appended.
func (h *ServiceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)

	return &ServiceHandler{
		svc:    h.svc,
		attrs:  newAttrs,
		groups: h.groups,
	}
}

// WithGroup returns a new ServiceHandler with the given group appended.
func (h *ServiceHandler) WithGroup(name string) slog.Handler {
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name

	return &ServiceHandler{
		svc:    h.svc,
		attrs:  h.attrs,
		groups: newGroups,
	}
}
