package console

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func stashedRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "one\n"})
	r.write("a.txt", "two\n")
	return r
}

func TestStashSavesAndListsEntries(t *testing.T) {
	r := stashedRepo(t)

	out := r.run(`stash push -m "work in progress"`)

	if !strings.HasPrefix(out, "Saved working directory as ") {
		t.Fatalf("out = %q", out)
	}
	if got := r.run("status --porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
	listed := lines(r.run("stash list"))
	if len(listed) != 1 || !strings.HasPrefix(listed[0], "stash@{0}: ") {
		t.Fatalf("list = %#v", listed)
	}
}

func TestStashWithoutASubcommandSaves(t *testing.T) {
	r := stashedRepo(t)

	r.run("stash")

	if got := lines(r.run("stash list")); len(got) != 1 {
		t.Fatalf("list = %#v", got)
	}
}

func TestStashSaveIsAnAliasOfPush(t *testing.T) {
	r := stashedRepo(t)

	r.run("stash save -m kept")

	if got := lines(r.run("stash list")); len(got) != 1 {
		t.Fatalf("list = %#v", got)
	}
}

func TestStashKeepsTheIndexOnRequest(t *testing.T) {
	r := stashedRepo(t)
	r.run("add a.txt")

	r.run("stash push -k")

	if got := r.run("status --porcelain"); got != "M  a.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestStashStoresUntrackedFilesOnRequest(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})
	r.write("fresh.txt", "fresh\n")

	r.run("stash push -u")

	if got := r.run("status --porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
}

func TestStashStoresOnlyTheStagedChangesOnRequest(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "one\n", "b.txt": "one\n"})
	r.write("a.txt", "two\n")
	r.write("b.txt", "two\n")
	r.run("add a.txt")

	r.run("stash push -S")

	if got := r.run("status --porcelain"); got != " M b.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestStashStoresOnlyTheNamedPaths(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "one\n", "b.txt": "one\n"})
	r.write("a.txt", "two\n")
	r.write("b.txt", "two\n")

	r.run("stash push -- a.txt")

	if got := r.run("status --porcelain"); got != " M b.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestStashApplyKeepsTheEntryAndPopDropsIt(t *testing.T) {
	r := stashedRepo(t)
	r.run("stash")

	if got := r.run("stash apply"); got != "Applied stash@{0}" {
		t.Fatalf("out = %q", got)
	}
	if got := lines(r.run("stash list")); len(got) != 1 {
		t.Fatalf("list = %#v", got)
	}
	r.run("checkout -f main")

	got := lines(r.run("stash pop"))
	if !slices.Equal(got, []string{"Applied stash@{0}", "Dropped stash@{0}"}) {
		t.Fatalf("out = %#v", got)
	}
	if got := r.run("stash list"); got != "" {
		t.Fatalf("list = %q", got)
	}
}

func TestStashApplyRestoresTheIndexOnRequest(t *testing.T) {
	r := stashedRepo(t)
	r.run("add a.txt")
	r.run("stash")

	r.run("stash apply --index")

	if got := r.run("status --porcelain"); got != "M  a.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestStashDropRemovesTheChosenEntry(t *testing.T) {
	r := stashedRepo(t)
	r.run("stash")
	r.write("a.txt", "three\n")
	r.run("stash")

	if got := r.run("stash drop stash@{1}"); got != "Dropped stash@{1}" {
		t.Fatalf("out = %q", got)
	}
	if got := lines(r.run("stash list")); len(got) != 1 {
		t.Fatalf("list = %#v", got)
	}
}

func TestStashTakesAPlainPositionToo(t *testing.T) {
	r := stashedRepo(t)
	r.run("stash")

	if got := r.run("stash drop 0"); got != "Dropped stash@{0}" {
		t.Fatalf("out = %q", got)
	}
}

func TestStashRefusesBadArguments(t *testing.T) {
	r := stashedRepo(t)
	r.run("stash")

	tests := []string{"stash nonsense", "stash list extra", "stash drop x", "stash drop -1", "stash drop 0 1", "stash apply 0 1"}
	for _, line := range tests {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
}

func TestStashCommandsOnAMissingEntryFail(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	for _, line := range []string{"stash apply", "stash drop"} {
		if err := r.runFails(line); err == nil {
			t.Fatalf("%q must fail", line)
		}
	}
}

func TestStashOfACleanTreeFails(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", map[string]string{"a.txt": "a\n"})

	if err := r.runFails("stash"); err == nil {
		t.Fatal("stashing a clean tree must fail")
	}
}
