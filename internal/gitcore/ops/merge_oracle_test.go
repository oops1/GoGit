//go:build oracle

package ops

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const mergeClockStart = 1700000000

type mergeBuilder struct {
	o     *oracle
	dir   string
	clock int64
}

func (b *mergeBuilder) dated() *oracle {
	b.clock += 60
	stamp := strconv.FormatInt(b.clock, 10) + " +0000"
	env := append(slices.Clone(b.o.env), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	return &oracle{t: b.o.t, home: b.o.home, env: env}
}

func (b *mergeBuilder) git(args ...string) string {
	b.o.t.Helper()
	return b.dated().run(b.dir, args...)
}

func (b *mergeBuilder) write(files map[string]string) {
	b.o.t.Helper()
	for rel, text := range files {
		if text == "" {
			b.git("rm", "-q", "-r", "--", rel)
			continue
		}
		b.o.write(b.dir, rel, text)
		b.git("add", "--", rel)
	}
}

func (b *mergeBuilder) commit(message string, files map[string]string) {
	b.o.t.Helper()
	b.write(files)
	b.git("commit", "-q", "--allow-empty", "-m", message)
}

func lines(seed string, count int) string {
	var out []string
	for i := range count {
		out = append(out, seed+" line "+strconv.Itoa(i)+" with enough words to be recognised")
	}
	return strings.Join(out, "\n") + "\n"
}

func editLine(text string, line int, replacement string) string {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	parts[line] = replacement
	return strings.Join(parts, "\n") + "\n"
}

type mergeScenario struct {
	name   string
	setup  func(b *mergeBuilder)
	target string
	args   []string
	opts   MergeOptions
	after  func(b *mergeBuilder, ours bool)
}

func forkedHistory(ours, theirs map[string]string) func(b *mergeBuilder) {
	return func(b *mergeBuilder) {
		b.commit("base", map[string]string{"keep": "keep\n", "f": lines("f", 10), "g": lines("g", 10)})
		b.git("branch", "feature")
		b.commit("ours", ours)
		b.git("checkout", "-q", "feature")
		b.commit("theirs", theirs)
		b.git("checkout", "-q", "main")
	}
}

func mergeScenarios() []mergeScenario {
	f, g := lines("f", 10), lines("g", 10)
	return []mergeScenario{
		{name: "fast-forward", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f})
			b.git("checkout", "-q", "-b", "feature")
			b.commit("next", map[string]string{"f": editLine(f, 1, "NEXT"), "dir/new": "new\n"})
			b.git("checkout", "-q", "main")
		}},
		{name: "already up to date", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f})
			b.git("branch", "feature")
			b.commit("ahead", map[string]string{"f": editLine(f, 1, "AHEAD")})
		}},
		{name: "different files", target: "feature", setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})},
		{name: "one file far apart", target: "feature", setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"f": editLine(f, 9, "THEIRS")})},
		{name: "content conflict", target: "feature", setup: forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": editLine(f, 4, "THEIRS")})},
		{name: "added differently", target: "feature", setup: forkedHistory(map[string]string{"new": "ours\n"}, map[string]string{"new": "theirs\n"})},
		{name: "modify against delete", target: "feature", setup: forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": ""})},
		{name: "delete against modify", target: "feature", setup: forkedHistory(map[string]string{"f": ""}, map[string]string{"f": editLine(f, 4, "THEIRS")})},
		{name: "added in a new directory", target: "feature", setup: forkedHistory(map[string]string{"a/b/c": "c\n"}, map[string]string{"x/y": "y\n", "g": ""})},
		{name: "no fast-forward", target: "feature", args: []string{"--no-ff"}, opts: MergeOptions{Mode: MergeNoFastForward}, setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f})
			b.git("checkout", "-q", "-b", "feature")
			b.commit("next", map[string]string{"g": g})
			b.git("checkout", "-q", "main")
		}},
		{name: "squash", target: "feature", args: []string{"--squash"}, opts: MergeOptions{Mode: MergeSquash}, setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})},
		{name: "squash with a conflict", target: "feature", args: []string{"--squash"}, opts: MergeOptions{Mode: MergeSquash}, setup: forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": editLine(f, 4, "THEIRS")})},
		{name: "no commit", target: "feature", args: []string{"--no-commit"}, opts: MergeOptions{NoCommit: true}, setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})},
		{name: "no commit, no fast-forward", target: "feature", args: []string{"--no-commit", "--no-ff"}, opts: MergeOptions{NoCommit: true, Mode: MergeNoFastForward}, setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})},
		{name: "into another branch", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("checkout", "-q", "-b", "dev")
			b.commit("dev", map[string]string{"dev": "dev\n"})
		}},
		{name: "a remote-tracking branch", target: "origin/feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("update-ref", "refs/remotes/origin/feature", "feature")
		}},
		{name: "a commit by its name", target: "feature~0", setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})},
		{name: "a rename against an edit", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.git("branch", "feature")
			b.git("mv", "f", "moved")
			b.commit("move", nil)
			b.git("checkout", "-q", "feature")
			b.commit("edit", map[string]string{"f": editLine(f, 9, "THEIRS")})
			b.git("checkout", "-q", "main")
		}},
		{name: "renamed apart", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.git("branch", "feature")
			b.git("mv", "f", "ours")
			b.commit("move", nil)
			b.git("checkout", "-q", "feature")
			b.git("mv", "f", "theirs")
			b.commit("move", nil)
			b.git("checkout", "-q", "main")
		}},
		{name: "a directory in the way", target: "feature", setup: forkedHistory(map[string]string{"d": "file\n"}, map[string]string{"d/x": "inside\n"})},
		{name: "criss-cross", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "g": g})
			b.git("branch", "feature")
			b.commit("ours 1", map[string]string{"f": editLine(f, 0, "OURS")})
			b.git("checkout", "-q", "feature")
			b.commit("theirs 1", map[string]string{"g": editLine(g, 0, "THEIRS")})
			b.git("merge", "-q", "--no-edit", "main")
			b.git("checkout", "-q", "main")
			b.git("merge", "-q", "--no-edit", "feature~1")
			b.commit("ours 2", map[string]string{"f": editLine(editLine(f, 0, "OURS"), 9, "OURS AGAIN")})
			b.git("checkout", "-q", "feature")
			b.commit("theirs 2", map[string]string{"g": editLine(editLine(g, 0, "THEIRS"), 9, "THEIRS AGAIN")})
			b.git("checkout", "-q", "main")
		}},
		{name: "fast-forward only refused", target: "feature", args: []string{"--ff-only"}, opts: MergeOptions{Mode: MergeFastForwardOnly}, setup: forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})},
		{name: "local change in the way", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.o.write(b.dir, "g", "local\n")
		}},
		{name: "local change elsewhere survives", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.o.write(b.dir, "keep", "local\n")
		}},
		{name: "staged change refused", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.write(map[string]string{"keep": "staged\n"})
		}},
		{name: "untracked file in the way", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"new": "theirs\n"})(b)
			b.o.write(b.dir, "new", "untracked\n")
		}},
		{name: "the early part of a branch", target: "feature~1", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("checkout", "-q", "feature")
			b.commit("later", map[string]string{"later": "later\n"})
			b.git("checkout", "-q", "main")
		}},
		{name: "the parent of a branch", target: "feature^", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("checkout", "-q", "feature")
			b.commit("later", map[string]string{"later": "later\n"})
			b.git("checkout", "-q", "main")
		}},
		{name: "into a detached head", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("checkout", "-q", "--detach", "main")
		}},
		{name: "into a branch the config suppresses", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("checkout", "-q", "-b", "release/1.0")
			b.git("config", "merge.suppressDest", "release/*")
		}},
		{name: "into main when the config lists other branches", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("config", "--add", "merge.suppressDest", "develop")
		}},
		{name: "into an unborn branch", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f})
			b.git("branch", "feature")
			b.git("checkout", "-q", "--orphan", "fresh")
			b.git("rm", "-q", "-r", "--cached", ".")
			if err := os.Remove(filepath.Join(b.dir, "f")); err != nil {
				b.o.t.Fatal(err)
			}
		}},
		{name: "a tag", target: "v1", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("tag", "v1", "feature")
		}},
		{name: "squash of a history with a merge", target: "feature", args: []string{"--squash"}, opts: MergeOptions{Mode: MergeSquash}, setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"g": editLine(g, 0, "THEIRS")})(b)
			b.git("checkout", "-q", "-b", "side", "feature~1")
			b.commit("side", map[string]string{"side": "side\n"})
			b.git("checkout", "-q", "feature")
			b.git("merge", "-q", "--no-edit", "side")
			b.commit("body", map[string]string{"body": "body\n"})
			b.git("commit", "-q", "--allow-empty", "-m", "subject", "-m", "first paragraph\n  indented\n\nlast")
			b.git("checkout", "-q", "main")
		}},
		{name: "binary conflict", target: "feature", setup: forkedHistory(map[string]string{"bin": "ours\x00"}, map[string]string{"bin": "theirs\x00"})},
		{name: "mode on one side", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(nil, map[string]string{"f": editLine(f, 9, "THEIRS")})(b)
			if err := os.Chmod(filepath.Join(b.dir, "f"), 0o755); err != nil {
				b.o.t.Fatal(err)
			}
			b.git("update-index", "--chmod=+x", "f")
			b.git("commit", "-q", "-m", "executable")
		}},
		{name: "rename against a delete", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.git("branch", "feature")
			b.git("mv", "f", "moved")
			b.commit("move", nil)
			b.git("checkout", "-q", "feature")
			b.commit("delete", map[string]string{"f": ""})
			b.git("checkout", "-q", "main")
		}},
		{name: "renamed apart with clashing edits", target: "feature", setup: func(b *mergeBuilder) {
			b.commit("base", map[string]string{"f": f, "keep": "keep\n"})
			b.git("branch", "feature")
			b.git("mv", "f", "ours")
			b.commit("move", map[string]string{"ours": editLine(f, 0, "OURS")})
			b.git("checkout", "-q", "feature")
			b.git("mv", "f", "theirs")
			b.commit("move", map[string]string{"theirs": editLine(f, 0, "THEIRS")})
			b.git("checkout", "-q", "main")
		}},
		{name: "conflicts in nested directories", target: "feature", setup: forkedHistory(
			map[string]string{"a/b/c": "ours\n", "a/d": editLine(f, 1, "OURS")},
			map[string]string{"a/b/c": "theirs\n", "a/d": editLine(f, 1, "THEIRS")})},
		{name: "line endings converted on checkout", target: "feature", setup: func(b *mergeBuilder) {
			b.git("config", "core.autocrlf", "true")
			forkedHistory(map[string]string{"f": editLine(f, 0, "OURS")}, map[string]string{"f": editLine(f, 9, "THEIRS"), "new": "one\ntwo\n"})(b)
		}},
		{name: "conflict resolved and committed", target: "feature", setup: forkedHistory(map[string]string{"f": editLine(f, 4, "OURS")}, map[string]string{"f": editLine(f, 4, "THEIRS")}), after: resolveAndCommit},
		{name: "conflict aborted", target: "feature", setup: func(b *mergeBuilder) {
			forkedHistory(map[string]string{"f": editLine(f, 4, "OURS"), "gone": "gone\n"}, map[string]string{"f": editLine(f, 4, "THEIRS"), "new": "new\n"})(b)
			b.o.write(b.dir, "keep", "local\n")
		}, after: abortMerge},
	}
}

func resolveAndCommit(b *mergeBuilder, ours bool) {
	b.o.t.Helper()
	b.o.write(b.dir, "f", "resolved\n")
	if !ours {
		b.o.run(b.dir, "add", "f")
		b.git("commit", "-q", "--no-edit", "--cleanup=strip")
		return
	}
	r := b.o.openRepo(b.dir)
	if err := Stage(b.o.t.Context(), r, []string{"f"}, StageOptions{}); err != nil {
		b.o.t.Fatalf("Stage: %v", err)
	}
	state, err := ReadMergeState(r)
	if err != nil {
		b.o.t.Fatalf("ReadMergeState: %v", err)
	}
	b.dated()
	if _, err := Commit(b.o.t.Context(), r, CommitOptions{Message: state.Message, When: time.Unix(b.clock, 0).UTC()}); err != nil {
		b.o.t.Fatalf("Commit: %v", err)
	}
}

func abortMerge(b *mergeBuilder, ours bool) {
	b.o.t.Helper()
	if !ours {
		b.git("merge", "--abort")
		return
	}
	if err := AbortMerge(b.o.t.Context(), b.o.openRepo(b.dir)); err != nil {
		b.o.t.Fatalf("AbortMerge: %v", err)
	}
}

func mergeStateOf(b *mergeBuilder, refused bool) string {
	b.o.t.Helper()
	var out []string
	add := func(label, text string) { out = append(out, "== "+label+"\n"+text) }
	add("HEAD", b.o.run(b.dir, "rev-parse", "HEAD"))
	add("message", b.o.run(b.dir, "log", "-1", "--format=%B%n%P%n%an %ae %ad%n%cn %ce %cd"))
	branch, _ := b.o.attempt(b.dir, "symbolic-ref", "-q", "HEAD")
	add("branch", branch)
	if !refused {
		add("reflog", b.o.run(b.dir, "reflog", "-1", "--format=%gs", "HEAD"))
	}
	add("index", b.o.run(b.dir, "ls-files", "-s"))
	add("status", b.o.run(b.dir, "status", "--porcelain", "-uall"))
	for _, name := range []string{mergeHeadFile, mergeMsgFile, mergeModeFile, origHeadFile, autoMergeFile, squashMsgFile} {
		if refused && name == autoMergeFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(b.dir, ".git", name))
		if errors.Is(err, fs.ErrNotExist) {
			add(name, "<none>")
			continue
		}
		add(name, string(data))
	}
	err := filepath.WalkDir(b.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		rel, _ := filepath.Rel(b.dir, path)
		add("file "+filepath.ToSlash(rel), string(data))
		return err
	})
	if err != nil {
		b.o.t.Fatal(err)
	}
	return strings.Join(out, "\n")
}

func sections(state string) map[string]string {
	out := map[string]string{}
	for part := range strings.SplitSeq(state, "== ") {
		label, body, _ := strings.Cut(part, "\n")
		out[label] = body
	}
	return out
}

func sectionDiff(got, want string) string {
	ours, theirs := sections(got), sections(want)
	var b strings.Builder
	for _, label := range slices.Sorted(maps.Keys(unionKeys(ours, theirs, map[string]bool{}))) {
		if ours[label] != theirs[label] {
			b.WriteString("== " + label + "\n-- ours:\n" + ours[label] + "\n-- git:\n" + theirs[label] + "\n")
		}
	}
	return b.String()
}

func TestOracleMergeLeavesTheRepositoryAsGitMergeDoes(t *testing.T) {
	for _, s := range mergeScenarios() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			sides := [2]*mergeBuilder{}
			for i, name := range []string{"git", "ours"} {
				dir := o.repoDir(name)
				newOracleRepo(o, dir)
				o.run(dir, "config", "core.autocrlf", "false")
				sides[i] = &mergeBuilder{o: o, dir: dir, clock: mergeClockStart}
				s.setup(sides[i])
			}
			gitSide, ourSide := sides[0], sides[1]

			_, gitErr := gitSide.dated().attempt(gitSide.dir, append(append([]string{"merge", "--no-edit"}, s.args...), s.target)...)
			ourSide.dated()
			opts := s.opts
			opts.When = time.Unix(ourSide.clock, 0).UTC()
			result, ourErr := Merge(t.Context(), o.openRepo(ourSide.dir), s.target, opts)
			gitFailed := gitErr != nil
			ourFailed := ourErr != nil || !result.Clean()
			if gitFailed != ourFailed {
				t.Fatalf("git failed = %v (%v), ours = %+v, %v", gitFailed, gitErr, result, ourErr)
			}
			if s.after != nil {
				s.after(gitSide, false)
				s.after(ourSide, true)
			}

			refused := ourErr != nil
			if got, want := mergeStateOf(ourSide, refused), mergeStateOf(gitSide, refused); got != want {
				t.Fatalf("repository differs from git's:\n%s", sectionDiff(got, want))
			}
		})
	}
}
