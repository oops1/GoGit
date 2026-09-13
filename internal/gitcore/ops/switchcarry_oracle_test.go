//go:build oracle

package ops

import (
	"testing"
)

func switchCarrySide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "a.txt", "shared\n")
	o.write(dir, "b.txt", "main\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.run(dir, "checkout", "-q", "-b", "topic")
	o.write(dir, "d.txt", "topic only\n")
	o.run(dir, "add", "d.txt")
	o.run(dir, "commit", "-q", "-m", "topic")
	o.run(dir, "checkout", "-q", "main")
	o.write(dir, "a.txt", "edited\n")
	o.write(dir, "b.txt", "staged\n")
	o.run(dir, "add", "b.txt")
	o.write(dir, "c.txt", "new\n")
	o.run(dir, "add", "c.txt")
	return dir
}

func TestOracleSwitchCarriesLocalChangesLikeGit(t *testing.T) {
	o := newOracle(t)
	gitSide := switchCarrySide(o, "git")
	ourSide := switchCarrySide(o, "ours")

	o.run(gitSide, "checkout", "-q", "topic")
	if err := Switch(t.Context(), o.openRepo(ourSide), "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}

	state := func(dir string) string {
		out := o.run(dir, "status", "--porcelain") + o.run(dir, "symbolic-ref", "HEAD")
		for _, rel := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
			out += rel + "=" + o.read(dir, rel)
		}
		return out
	}
	if got, want := state(ourSide), state(gitSide); got != want {
		t.Fatalf("after the switch\nours:\n%s\ngit:\n%s", got, want)
	}
}
