package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/zisen123/serialgateway/internal/config"
)

func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level: %q (valid: debug, info, warn, error)", s)
	}
}

const (
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorCyan   = "\033[36m"
	colorReset  = "\033[0m"
)

func levelColor(level slog.Level) string {
	switch level {
	case slog.LevelError:
		return colorRed
	case slog.LevelWarn:
		return colorYellow
	case slog.LevelInfo:
		return colorGreen
	case slog.LevelDebug:
		return colorCyan
	default:
		return ""
	}
}

func levelLabel(level slog.Level) string {
	switch level {
	case slog.LevelError:
		return "ERROR"
	case slog.LevelWarn:
		return "WARN "
	case slog.LevelInfo:
		return "INFO "
	case slog.LevelDebug:
		return "DEBUG"
	default:
		return "     "
	}
}

type compactHandler struct {
	level    slog.Leveler
	w        io.Writer
	color    bool
	mu       sync.Mutex
	preAttrs []slog.Attr
	group    string
}

func (h *compactHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *compactHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var buf strings.Builder

	// time
	buf.WriteString(r.Time.Format("15:04:05.000"))
	buf.WriteByte(' ')

	// level with optional color
	lvl := r.Level
	if h.color {
		buf.WriteString(levelColor(lvl))
	}
	buf.WriteString(levelLabel(lvl))
	if h.color {
		buf.WriteString(colorReset)
	}
	buf.WriteByte(' ')

	// message
	buf.WriteString(r.Message)

	// pre-attached attrs (from WithAttrs)
	for _, a := range h.preAttrs {
		buf.WriteByte(' ')
		h.writeAttr(&buf, a)
	}

	// record attrs (from the log call)
	r.Attrs(func(a slog.Attr) bool {
		buf.WriteByte(' ')
		h.writeAttr(&buf, a)
		return true
	})

	buf.WriteByte('\n')

	_, err := h.w.Write([]byte(buf.String()))
	return err
}

func (h *compactHandler) writeAttr(buf *strings.Builder, a slog.Attr) {
	key := a.Key
	if h.group != "" {
		key = h.group + "." + key
	}
	buf.WriteString(key)
	buf.WriteByte('=')
	buf.WriteString(a.Value.String())
}

func (h *compactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.preAttrs = make([]slog.Attr, len(h.preAttrs)+len(attrs))
	copy(h2.preAttrs, h.preAttrs)
	copy(h2.preAttrs[len(h.preAttrs):], attrs)
	return &h2
}

func (h *compactHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := *h
	if h2.group != "" {
		h2.group = h2.group + "." + name
	} else {
		h2.group = name
	}
	return &h2
}

func New(cfg *config.LoggingConfig) (*slog.Logger, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	var w io.Writer
	var color bool
	switch cfg.Output {
	case "stdout":
		w = os.Stdout
		color = true
	case "stderr":
		w = os.Stderr
		color = true
	case "file":
		w = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSizeMB,
			MaxAge:     cfg.MaxAgeDays,
			MaxBackups: cfg.MaxBackups,
			Compress:   cfg.Compress,
			LocalTime:  true,
		}
	default:
		return nil, fmt.Errorf("unknown log output: %q (valid: stdout, stderr, file)", cfg.Output)
	}

	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	case "text":
		handler = &compactHandler{
			level: level,
			w:     w,
			color: color,
		}
	default:
		return nil, fmt.Errorf("unknown log format: %q (valid: text, json)", cfg.Format)
	}

	return slog.New(handler), nil
}