package console

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

func TestTagCreatesAndListsTags(t *testing.T) {
	r := newTestRepo(t)
	head := r.commit("initial", map[string]string{"a.txt": "a\n"})

	out := r.run("tag v1")

	if out != "Created tag v1 at "+head.String()[:shortHashLength] {
		t.Fatalf("out = %q", out)
	}
	if got := r.run("tag"); got != "v1" {
		t.Fatalf("tag = %q", got)
	}
	if got := r.run("tag -l"); got != "v1" {
		t.Fatalf("tag -l = %q", got)
	}
}

func TestTagTakesAnExplicitTarget(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("first", map[string]string{"a.txt": "a\n"})
	r.commit("second", map[string]string{"b.txt": "b\n"})

	r.run("tag v1 " + first.String())

	if got := r.run("tag"); got != "v1" {
		t.Fatalf("tag = %q", got)
	}
}

func TestTagWritesAnAnnotatedTagWithAMessage(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	r.run(`tag -a v1 -m "the release"`)

	tags, err := ops.Tags(t.Context(), r.repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || !tags[0].Annotated || tags[0].Subject != "the release" {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestAnAnnotatedTagNeedsAMessage(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("tag -a v1"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestTagForcesAnExistingName(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit("first", map[string]string{"a.txt": "a\n"})
	r.commit("second", map[string]string{"b.txt": "b\n"})
	r.run("tag v1 " + first.String())

	if err := r.runFails("tag v1"); err == nil {
		t.Fatal("reusing a tag name must fail")
	}
	r.run("tag -f v1")
}

func TestTagDeletesTags(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.run("tag v1")
	r.run("tag v2")

	got := lines(r.run("tag -d v1 v2"))

	if !slices.Equal(got, []string{"Deleted tag v1", "Deleted tag v2"}) {
		t.Fatalf("out = %#v", got)
	}
	if got := r.run("tag"); got != "" {
		t.Fatalf("tag = %q", got)
	}
}

func TestTagDeleteNeedsANameAndReportsFailures(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("tag -d"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
	if err := r.runFails("tag -d missing"); err == nil {
		t.Fatal("deleting a missing tag must fail")
	}
}

func TestTagRefusesTooManyNames(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("tag a b c"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestTagRefusesAnInvalidName(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	err := r.runFails("tag ..bad")
	if !errors.Is(err, ops.ErrInvalidTagName) || !strings.Contains(err.Error(), "..bad") {
		t.Fatalf("err = %v", err)
	}
}
