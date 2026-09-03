package logx

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestOpenCreatesFileAndWritesThroughSlog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "app.log")
	l, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Slog().Info("started")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "msg=started") {
		t.Fatalf("missing message: %s", data)
	}
}

func TestOpenFailsWhenParentIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(blocker, "sub", "app.log"), Options{}); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestOpenFailsWhenPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, Options{}); err == nil {
		t.Fatal("expected open error")
	}
}

func TestOpenResumesExistingFileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path, Options{MaxSize: 5, Keep: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := l.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "0123456789" {
		t.Fatalf("backup = %q", backup)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "x" {
		t.Fatalf("current = %q", current)
	}
}

func TestSlogWritesRFC3339Time(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Slog().Info("started", "version", "1.0")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "version=1.0") {
		t.Fatalf("missing attr: %s", data)
	}
	re := regexp.MustCompile(`time=\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})`)
	if !re.MatchString(string(data)) {
		t.Fatalf("time not RFC3339: %s", data)
	}
}

func TestMirrorReceivesCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	var buf bytes.Buffer
	l, err := Open(path, Options{Mirror: &buf})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Slog().Info("mirrored")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mirrored") || !strings.Contains(buf.String(), "mirrored") {
		t.Fatalf("mirror mismatch: file=%q mirror=%q", data, buf.String())
	}
}

func TestDefaultLevelIsInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Slog().Debug("hidden")
	l.Slog().Info("shown")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hidden") {
		t.Fatalf("debug line leaked: %s", data)
	}
	if !strings.Contains(string(data), "shown") {
		t.Fatalf("info line missing: %s", data)
	}
}

func TestCustomLevelDebug(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{Level: slog.LevelDebug})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Slog().Debug("visible")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "visible") {
		t.Fatalf("debug missing: %s", data)
	}
}

func TestPathReturnsConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if l.Path() != path {
		t.Fatalf("path = %q, want %q", l.Path(), path)
	}
}

func TestWriteFailsAfterClose(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "app.log"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second close = %v, want nil", err)
	}
	if _, err := l.Write([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestDiscardLoggerNeverWrites(t *testing.T) {
	l := Discard()
	l.Slog().Info("hello", "password", "x")
	if l.Path() != "" {
		t.Fatalf("path = %q", l.Path())
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRotationCreatesBackupsAndTrimsExcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{MaxSize: 5, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, chunk := range []string{"aaaaa", "bbbbb", "ccccc", "ddddd"} {
		if _, err := l.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]string{
		path:        "ddddd",
		path + ".1": "ccccc",
		path + ".2": "bbbbb",
	}
	for name, want := range cases {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(path + ".3"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("extra backup should not exist: %v", err)
	}
}

func TestRotateFailsWhenFileAlreadyClosed(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "app.log"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.rotate(); err == nil {
		t.Fatal("expected close error")
	}
}

func TestRotateFailsWhenShiftRenameBlocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{MaxSize: 1, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := os.WriteFile(path+".1", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".2", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("xx")); err == nil {
		t.Fatal("expected rename error")
	}
}

func TestRotateFailsWhenFinalRenameBlocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{MaxSize: 1, Keep: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := os.Mkdir(path+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("xx")); err == nil {
		t.Fatal("expected rename error")
	}
}

func TestReopenFailsWhenPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "app.log"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	l.path = blocked
	if err := l.reopen(); err == nil {
		t.Fatal("expected reopen error")
	}
}

func TestConcurrentWritesRace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	l, err := Open(path, Options{MaxSize: 200, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			l.Slog().Info("concurrent", "n", i)
		})
	}
	wg.Wait()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}
