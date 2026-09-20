package console

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func TestConfigReadsAValue(t *testing.T) {
	r := newTestRepo(t)

	if got := r.run("config user.name"); got != "Go Git" {
		t.Fatalf("value = %q", got)
	}
	if got := r.run("config --get user.email"); got != "gogit@example.com" {
		t.Fatalf("value = %q", got)
	}
}

func TestConfigWritesAValue(t *testing.T) {
	r := newTestRepo(t)

	if got := r.run("config core.autocrlf false"); got != "core.autocrlf=false" {
		t.Fatalf("out = %q", got)
	}
	r.reopen()

	if got := r.run("config core.autocrlf"); got != "false" {
		t.Fatalf("value = %q", got)
	}
}

func TestConfigUnsetsAValue(t *testing.T) {
	r := newTestRepo(t)
	r.run("config core.autocrlf false")
	r.reopen()

	if got := r.run("config --unset core.autocrlf"); got != "Unset core.autocrlf" {
		t.Fatalf("out = %q", got)
	}
	r.reopen()

	if err := r.runFails("config core.autocrlf"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigListsEveryVariable(t *testing.T) {
	r := newTestRepo(t)

	got := lines(r.run("config --list"))

	if !slices.Contains(got, "user.name=Go Git") {
		t.Fatalf("list = %#v", got)
	}
}

func TestConfigRefusesBadArguments(t *testing.T) {
	r := newTestRepo(t)

	for _, line := range []string{"config", "config a b c"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestConfigNeedsALocalFileToWrite(t *testing.T) {
	r := newTestRepo(t)
	if err := os.Remove(r.repo.CommonPath("config")); err != nil {
		t.Fatal(err)
	}
	r.reopen()

	for _, line := range []string{"config core.autocrlf false", "config --unset core.autocrlf"} {
		if err := r.runFails(line); !errors.Is(err, ErrNoLocalConfig) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestConfigRefusesAKeyWithoutASection(t *testing.T) {
	r := newTestRepo(t)

	for _, line := range []string{"config bare true", "config --unset bare"} {
		if err := r.runFails(line); err == nil {
			t.Fatalf("%q must fail", line)
		}
	}
}

func TestConfigReportsAFailedSave(t *testing.T) {
	r := newTestRepo(t)
	wantErr := errors.New("boom")
	prev := saveConfig
	saveConfig = func(*config.File) error { return wantErr }
	t.Cleanup(func() { saveConfig = prev })

	for _, line := range []string{"config core.autocrlf false", "config --unset core.autocrlf"} {
		if err := r.runFails(line); !errors.Is(err, wantErr) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}
