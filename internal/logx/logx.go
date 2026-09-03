package logx

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	defaultMaxSize = 5 * 1024 * 1024
	defaultKeep    = 3
)

var ErrClosed = errors.New("logx: logger closed")

type Options struct {
	Level      slog.Level
	Mirror     io.Writer
	MaxSize    int64
	Keep       int
	RedactKeys []string
}

type Logger struct {
	mu      sync.Mutex
	f       *os.File
	path    string
	size    int64
	maxSize int64
	keep    int
	closed  bool
	sl      *slog.Logger
}

func Open(path string, opts Options) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("logx: create dir: %w", err)
	}
	size := fileSize(path)
	f, err := openLogFile(path)
	if err != nil {
		return nil, fmt.Errorf("logx: open file: %w", err)
	}
	l := &Logger{
		f:       f,
		path:    path,
		size:    size,
		maxSize: withDefault(opts.MaxSize, int64(defaultMaxSize)),
		keep:    withDefault(opts.Keep, defaultKeep),
	}
	var w io.Writer = l
	if opts.Mirror != nil {
		w = io.MultiWriter(l, opts.Mirror)
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       opts.Level,
		ReplaceAttr: replaceAttr,
	})
	l.sl = slog.New(Redact(handler, opts.RedactKeys...))
	return l, nil
}

func Discard() *Logger {
	return &Logger{
		sl:     slog.New(slog.DiscardHandler),
		closed: true,
	}
}

func withDefault[T int | int64](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func openLogFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.String(slog.TimeKey, a.Value.Time().Format(time.RFC3339))
	}
	return a
}

func (l *Logger) Slog() *slog.Logger { return l.sl }

func (l *Logger) Path() string { return l.path }

func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, ErrClosed
	}
	if l.size+int64(len(p)) > l.maxSize {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *Logger) rotate() error {
	if err := l.f.Close(); err != nil {
		return err
	}
	for i := l.keep - 1; i >= 1; i-- {
		if err := renameIfExists(l.rotatedPath(i), l.rotatedPath(i+1)); err != nil {
			return err
		}
	}
	if err := renameIfExists(l.path, l.rotatedPath(1)); err != nil {
		return err
	}
	l.size = 0
	return l.reopen()
}

func (l *Logger) reopen() error {
	f, err := openLogFile(l.path)
	if err != nil {
		return err
	}
	l.f = f
	return nil
}

func (l *Logger) rotatedPath(n int) string {
	return l.path + "." + strconv.Itoa(n)
}

func renameIfExists(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.f.Close()
}
