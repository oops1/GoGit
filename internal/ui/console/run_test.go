package console

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestRunIgnoresAnEmptyLine(t *testing.T) {
	out, err := Run(t.Context(), Env{}, "   ")
	if err != nil || out != "" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestRunReportsAnUnknownCommand(t *testing.T) {
	_, err := Run(t.Context(), Env{}, "frobnicate")
	if !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("err = %v", err)
	}
	var d *Detail
	if !errors.As(err, &d) || d.Text != "frobnicate" {
		t.Fatalf("detail = %+v", d)
	}
}

func TestRunSaysWhichGitCommandsAreNotSupportedYet(t *testing.T) {
	for _, name := range unsupportedCommands {
		_, err := Run(t.Context(), Env{}, name)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestRunAsksForARepositoryBeforeWorking(t *testing.T) {
	if _, err := Run(t.Context(), Env{}, "status"); !errors.Is(err, ErrNoRepository) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunPassesTheParseErrorThrough(t *testing.T) {
	if _, err := Run(t.Context(), Env{}, "commit -m 'oops"); !errors.Is(err, ErrBadQuoting) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunStopsOnACancelledContext(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Run(ctx, r.env, "status"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestHelpListsEverySupportedCommand(t *testing.T) {
	out, err := Run(t.Context(), Env{}, "help")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lines(out), Names()) {
		t.Fatalf("help = %q", out)
	}
	if !slices.IsSorted(Names()) || !slices.Contains(Names(), "status") {
		t.Fatalf("names = %v", Names())
	}
}

func TestEveryNamedCommandRunsAndReportsItsOwnUsage(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	for _, name := range Names() {
		if _, err := Run(t.Context(), r.env, name+" --definitely-not-an-option"); !errors.Is(err, ErrUnknownOption) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestEnvFallsBackToTheWallClockAndAnEmptyTransport(t *testing.T) {
	env := Env{}
	if env.when().IsZero() {
		t.Fatal("when() must give a real time")
	}
	if env.network().UserAgent != "" {
		t.Fatal("network() must be empty without a provider")
	}
	env.Now = time.Unix(testClock, 0).UTC()
	env.Transport = func(progress.Func) transport.Options {
		return transport.Options{UserAgent: "test"}
	}
	if !env.when().Equal(time.Unix(testClock, 0).UTC()) || env.network().UserAgent != "test" {
		t.Fatalf("when = %v, agent = %q", env.when(), env.network().UserAgent)
	}
}

func TestStageIsAnAliasOfAdd(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", "a\n")

	out := r.run("stage a.txt")

	if !strings.HasPrefix(out, "Staged ") {
		t.Fatalf("out = %q", out)
	}
	if got := r.run("status --porcelain"); got != "A  a.txt" {
		t.Fatalf("status = %q", got)
	}
}
