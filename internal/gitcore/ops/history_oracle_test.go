//go:build oracle

package ops

import (
	"strings"
	"testing"
)

type historyCase struct {
	name   string
	build  func(b *mergeBuilder)
	path   string
	follow bool
}

func historyCases() []historyCase {
	f := lines("f", 10)
	return []historyCase{
		{name: "a file touched twice", path: "f", build: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.commit("edit", map[string]string{"f": editLine(f, 4, "EDITED")})
			b.commit("elsewhere", map[string]string{"keep": "changed\n"})
		}},
		{name: "a file in a directory", path: "dir/f", build: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"dir/f": f, "keep": "keep\n"})
			b.commit("edit", map[string]string{"dir/f": editLine(f, 1, "EDITED")})
		}},
		{name: "a file that came with a merge", path: "f", build: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"keep": "keep\n"})
			b.git("branch", "feature")
			b.commit("ours", map[string]string{"keep": "ours\n"})
			b.git("checkout", "-q", "feature")
			b.commit("theirs", map[string]string{"f": f})
			b.git("checkout", "-q", "main")
			b.dated().run(b.dir, "merge", "--no-edit", "feature")
		}},
		{name: "a file that was deleted and brought back", path: "f", build: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.commit("remove", map[string]string{"f": ""})
			b.commit("bring back", map[string]string{"f": f})
		}},
		{name: "a renamed file", path: "moved", follow: true, build: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.git("mv", "f", "moved")
			b.git("commit", "-q", "-m", "move")
			b.commit("edit", map[string]string{"moved": editLine(f, 6, "EDITED")})
		}},
		{name: "a file renamed twice", path: "final", follow: true, build: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f})
			b.git("mv", "f", "middle")
			b.git("commit", "-q", "-m", "first move")
			b.git("mv", "middle", "final")
			b.git("commit", "-q", "-m", "second move")
		}},
	}
}

func gitHistory(t *testing.T, b *mergeBuilder, path string, follow bool) []string {
	t.Helper()
	args := []string{"log", "--format=%H"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, "--", path)
	var out []string
	for line := range strings.SplitSeq(strings.TrimSpace(b.o.run(b.dir, args...)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func ourHistory(t *testing.T, o *oracle, b *mergeBuilder, path string, follow bool) []string {
	t.Helper()
	entries, err := FileHistory(t.Context(), o.openRepo(b.dir), "HEAD", path, HistoryOptions{Follow: follow})
	if err != nil {
		t.Fatalf("FileHistory returned error %v", err)
	}
	var out []string
	for _, entry := range entries {
		out = append(out, entry.Commit.String())
	}
	return out
}

func TestOracleFileHistoryMatchesGitLog(t *testing.T) {
	for _, c := range historyCases() {
		t.Run(c.name, func(t *testing.T) {
			o := newOracle(t)
			dir := o.repoDir("work")
			newOracleRepo(o, dir)
			o.run(dir, "config", "core.autocrlf", "false")
			b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
			c.build(b)

			got, want := ourHistory(t, o, b, c.path, c.follow), gitHistory(t, b, c.path, c.follow)

			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Fatalf("history = %v, git says %v", got, want)
			}
		})
	}
}

func TestOracleBlameThroughTheOpsAPIMatchesGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	b := &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
	f := lines("f", 10)
	b.commit("base", map[string]string{"f": f})
	b.commit("edit", map[string]string{"f": editLine(f, 3, "EDITED")})

	result, err := Blame(t.Context(), o.openRepo(dir), "HEAD", "f", BlameOptions{})
	if err != nil {
		t.Fatalf("Blame returned error %v", err)
	}

	var got []string
	for _, line := range result.Lines {
		got = append(got, line.Commit.String())
	}
	var want []string
	for block := range strings.SplitSeq(b.o.run(dir, "blame", "--porcelain", "HEAD", "--", "f"), "\n") {
		fields := strings.Fields(block)
		if len(fields) >= 3 && len(fields[0]) == 40 {
			want = append(want, fields[0])
		}
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("blame = %v, git says %v", got, want)
	}
}
