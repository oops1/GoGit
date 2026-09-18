//go:build oracle

package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

func stashRichSide(o *oracle, name string) string {
	o.t.Helper()
	dir := stashSide(o, name)
	o.write(dir, ".git/info/exclude", "*.log\nbuild/\n")
	o.write(dir, "debug.log", "log\n")
	o.write(dir, "build/out.bin", "bin\n")
	o.write(dir, "tools/nested/tool.txt", "tool\n")
	o.write(dir, "tools/nested/trace.log", "trace\n")
	if err := os.MkdirAll(filepath.Join(dir, "empty", "inner"), 0o777); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	return dir
}

func stashLinesSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "m.txt", numberedLines(1, 12))
	o.write(dir, "keep.txt", "keep\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "lines")
	o.write(dir, "m.txt", strings.Replace(numberedLines(1, 12), "l3\n", "X3\n", 1))
	o.run(dir, "add", "m.txt")
	return dir
}

func numberedLines(from, to int) string {
	var text strings.Builder
	for n := from; n <= to; n++ {
		text.WriteString("l" + strconv.Itoa(n) + "\n")
	}
	return text.String()
}

func stashRepoState(o *oracle, dir string) string {
	o.t.Helper()
	var state strings.Builder
	for _, args := range [][]string{
		{"rev-parse", "-q", "--verify", "stash"},
		{"rev-parse", "-q", "--verify", "stash^2"},
		{"rev-parse", "-q", "--verify", "stash^3"},
		{"stash", "list"},
		{"status", "--porcelain", "--ignored", "--untracked-files=all"},
		{"ls-files", "-s"},
		{"log", "-g", "--format=%H %gs", "HEAD"},
		{"log", "-g", "--format=%H %gs", "refs/stash"},
		{"rev-parse", "-q", "--verify", "ORIG_HEAD"},
	} {
		out, err := o.attempt(dir, args...)
		state.WriteString("$ git " + strings.Join(args, " ") + "\n" + out)
		if err != nil {
			state.WriteString("(failed)\n")
		}
	}
	state.WriteString("$ files\n" + worktreeListing(o, dir))
	return state.String()
}

func worktreeListing(o *oracle, dir string) string {
	o.t.Helper()
	var lines []string
	tree := os.DirFS(dir)
	err := fs.WalkDir(tree, ".", func(rel string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case rel == ".":
			return nil
		case entry.Name() == ".git":
			return fs.SkipDir
		case entry.IsDir():
			lines = append(lines, "dir "+rel)
			return nil
		}
		data, err := fs.ReadFile(tree, rel)
		lines = append(lines, "file "+rel+" "+strings.ReplaceAll(string(data), "\n", "\\n"))
		return err
	})
	if err != nil {
		o.t.Fatalf("WalkDir returned error %v", err)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n") + "\n"
}

func requireSameStashState(o *oracle, gitSide, ourSide string) {
	o.t.Helper()
	if want, got := stashRepoState(o, gitSide), stashRepoState(o, ourSide); want != got {
		o.t.Fatalf("repository state differs from git:\ngit:\n%s\nours:\n%s", want, got)
	}
}

type stashPushCase struct {
	name              string
	side              func(*oracle, string) string
	prepare           func(*oracle, string)
	args              []string
	opts              StashOptions
	gitFails          bool
	wantErr           error
	needsBinaryStaged bool
}

func gitStashesStagedBinaryContent(o *oracle) bool {
	dir := stashBinarySide(o, "binary-staged-probe")
	_, err := o.attempt(dir, "stash", "push", "-q", "--staged")
	return err == nil
}

func stashPushCases() []stashPushCase {
	return []stashPushCase{
		{name: "include untracked", args: []string{"-u"}, opts: StashOptions{IncludeUntracked: true}},
		{name: "include ignored", args: []string{"-a"}, opts: StashOptions{IncludeIgnored: true}},
		{name: "untracked with a message", args: []string{"-u", "-m", "note"}, opts: StashOptions{IncludeUntracked: true, Message: "note"}},
		{name: "keep index", args: []string{"--keep-index"}, opts: StashOptions{KeepIndex: true}},
		{name: "keep index with untracked", args: []string{"--keep-index", "-u"}, opts: StashOptions{KeepIndex: true, IncludeUntracked: true}},
		{name: "staged", args: []string{"--staged"}, opts: StashOptions{Staged: true}},
		{name: "staged keeping the index", args: []string{"--staged", "--keep-index"}, opts: StashOptions{Staged: true, KeepIndex: true}},
		{name: "staged paths", args: []string{"--staged", "--", "b.txt"}, opts: StashOptions{Staged: true, Paths: []string{"b.txt"}}},
		{name: "paths", args: []string{"--", "b.txt", "new.txt"}, opts: StashOptions{Paths: []string{"b.txt", "new.txt"}}},
		{name: "unstaged path", args: []string{"--", "a.txt"}, opts: StashOptions{Paths: []string{"a.txt"}}},
		{name: "path deleted from the work tree", args: []string{"--", "d.txt"}, opts: StashOptions{Paths: []string{"d.txt"}}},
		{name: "path deleted from the index", args: []string{"--", "c.txt", "d.txt"}, opts: StashOptions{Paths: []string{"c.txt", "d.txt"}}, gitFails: true, wantErr: ErrPathspecNoMatch},
		{name: "glob path", args: []string{"--", "*.txt"}, opts: StashOptions{Paths: []string{"*.txt"}}},
		{name: "untracked directory path", args: []string{"-u", "--", "tools"}, opts: StashOptions{IncludeUntracked: true, Paths: []string{"tools"}}},
		{name: "untracked and tracked paths", args: []string{"-u", "--", "untracked.txt", "a.txt"}, opts: StashOptions{IncludeUntracked: true, Paths: []string{"untracked.txt", "a.txt"}}},
		{name: "ignored path", args: []string{"-a", "--", "build"}, opts: StashOptions{IncludeIgnored: true, Paths: []string{"build"}}},
		{name: "keep index paths", args: []string{"--keep-index", "--", "b.txt", "a.txt"}, opts: StashOptions{KeepIndex: true, Paths: []string{"b.txt", "a.txt"}}},
		{name: "unknown path", args: []string{"--", "missing.txt"}, opts: StashOptions{Paths: []string{"missing.txt"}}, gitFails: true, wantErr: ErrPathspecNoMatch},
		{
			name:    "only untracked files",
			prepare: func(o *oracle, dir string) { o.run(dir, "reset", "-q", "--hard") },
			args:    []string{"-u"}, opts: StashOptions{IncludeUntracked: true},
		},
		{
			name:    "only untracked files without the option",
			prepare: func(o *oracle, dir string) { o.run(dir, "reset", "-q", "--hard") },
			wantErr: ErrNothingToStash,
		},
		{
			name:    "staged without staged changes",
			prepare: func(o *oracle, dir string) { o.run(dir, "reset", "-q") },
			args:    []string{"--staged"}, opts: StashOptions{Staged: true},
			gitFails: true, wantErr: ErrNoStagedChanges,
		},
		{
			name:    "staged over a later edit of the same lines",
			prepare: func(o *oracle, dir string) { o.write(dir, "b.txt", "b3\n") },
			args:    []string{"--staged"}, opts: StashOptions{Staged: true},
			gitFails: true, wantErr: ErrStashWorktreeKept,
		},
		{
			name: "staged beside later edits elsewhere",
			side: stashLinesSide,
			prepare: func(o *oracle, dir string) {
				text := strings.Replace(numberedLines(1, 12), "l3\n", "X3\n", 1)
				text = "top1\ntop2\n" + strings.Replace(text, "l10\n", "Y10\n", 1)
				o.write(dir, "m.txt", text)
			},
			args: []string{"--staged"}, opts: StashOptions{Staged: true},
		},
		{
			name: "staged beside an adjacent later edit",
			side: stashLinesSide,
			prepare: func(o *oracle, dir string) {
				o.write(dir, "m.txt", strings.Replace(strings.Replace(numberedLines(1, 12), "l3\n", "X3\n", 1), "l4\n", "Y4\n", 1))
			},
			args: []string{"--staged"}, opts: StashOptions{Staged: true},
			gitFails: true, wantErr: ErrStashWorktreeKept,
		},
		{name: "untracked beside nested repositories", side: stashNestedSide, args: []string{"-u"}, opts: StashOptions{IncludeUntracked: true}},
		{name: "ignored beside nested repositories", side: stashNestedSide, args: []string{"-a"}, opts: StashOptions{IncludeIgnored: true}},
		{
			name: "only a nested repository", side: stashNestedSide,
			prepare: func(o *oracle, dir string) {
				o.run(dir, "reset", "-q", "--hard")
				o.run(dir, "clean", "-q", "-f", "-d", "--", "untracked.txt", "tools", "empty", "fresh")
			},
			args: []string{"-u"}, opts: StashOptions{IncludeUntracked: true},
		},
		{name: "nested repository path", side: stashNestedSide, args: []string{"-u", "--", "vendor", "a.txt"}, opts: StashOptions{IncludeUntracked: true, Paths: []string{"vendor", "a.txt"}}},
		{name: "staged binary edit", side: stashBinarySide, args: []string{"--staged"}, opts: StashOptions{Staged: true}, needsBinaryStaged: true},
		{
			name: "staged new binary file", side: stashBinarySide,
			prepare: func(o *oracle, dir string) {
				o.run(dir, "reset", "-q")
				stageText(o, dir, "fresh.bin", "fresh\x00bin\n")
			},
			args: []string{"--staged"}, opts: StashOptions{Staged: true}, needsBinaryStaged: true,
		},
		{
			name: "staged binary mode change", side: stashBinarySide,
			prepare: func(o *oracle, dir string) {
				o.run(dir, "reset", "-q", "--hard")
				o.run(dir, "update-index", "--chmod=+x", "bin")
			},
			args: []string{"--staged"}, opts: StashOptions{Staged: true},
		},
		{name: "staged binary made text by attributes", side: stashBinarySide, prepare: markBinaryAsText, args: []string{"--staged"}, opts: StashOptions{Staged: true}},
		{name: "staged symbolic link", side: stashSymlinkSide, args: []string{"--staged"}, opts: StashOptions{Staged: true}},
		{
			name: "staged symbolic link retargeted again", side: stashSymlinkSide,
			prepare: func(o *oracle, dir string) { relink(o, dir, "c.txt") },
			args:    []string{"--staged"}, opts: StashOptions{Staged: true}, gitFails: true, wantErr: ErrStashWorktreeKept,
		},
		{
			name: "staged symbolic link replaced by a file", side: stashSymlinkSide,
			prepare: func(o *oracle, dir string) {
				o.remove(dir, "link")
				o.write(dir, "link", "b.txt")
			},
			args: []string{"--staged"}, opts: StashOptions{Staged: true}, gitFails: true, wantErr: ErrStashWorktreeKept,
		},
	}
}

func stashNestedSide(o *oracle, name string) string {
	o.t.Helper()
	dir := stashRichSide(o, name)
	for _, nested := range []string{"vendor/lib", "build/cache"} {
		path := filepath.Join(dir, filepath.FromSlash(nested))
		if err := os.MkdirAll(path, 0o777); err != nil {
			o.t.Fatalf("MkdirAll returned error %v", err)
		}
		newOracleRepo(o, path)
		o.write(path, "code.txt", "code\n")
		o.run(path, "add", ".")
		o.run(path, "commit", "-q", "-m", "nested")
	}
	nested := filepath.Join(dir, "fresh", "unborn")
	if err := os.MkdirAll(nested, 0o777); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	newOracleRepo(o, nested)
	o.write(nested, "draft.txt", "draft\n")
	return dir
}

func stashBinarySide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, "a.txt", "a\n")
	o.write(dir, "bin", "bin\x00base\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "binary")
	stageText(o, dir, "bin", "bin\x00staged\n")
	return dir
}

func markBinaryAsText(o *oracle, dir string) {
	o.write(dir, ".git/info/attributes", "bin diff\n")
}

func stashSymlinkSide(o *oracle, name string) string {
	o.t.Helper()
	dir := o.repoDir(name)
	newOracleRepo(o, dir)
	if strings.TrimSpace(o.run(dir, "config", "--get", "--default", "true", "core.symlinks")) != "true" {
		o.t.Skip("git does not create symbolic links here")
	}
	for _, file := range []string{"a.txt", "b.txt", "c.txt"} {
		o.write(dir, file, file+"\n")
	}
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		o.t.Skipf("symbolic links are not supported: %v", err)
	}
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "link")
	relink(o, dir, "b.txt")
	o.run(dir, "add", "link")
	return dir
}

func relink(o *oracle, dir, target string) {
	o.t.Helper()
	o.remove(dir, "link")
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		o.t.Fatalf("Symlink returned error %v", err)
	}
}

func TestOracleStashPushOptionsMatchGit(t *testing.T) {
	binaryStaged := gitStashesStagedBinaryContent(newStashOracle(t))
	for _, tc := range stashPushCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsBinaryStaged && !binaryStaged {
				t.Skip("the installed git refuses to stash staged binary content")
			}
			o := newStashOracle(t)
			side := tc.side
			if side == nil {
				side = stashRichSide
			}
			gitSide, ourSide := side(o, "git"), side(o, "ours")
			if tc.prepare != nil {
				tc.prepare(o, gitSide)
				tc.prepare(o, ourSide)
			}
			_, gitErr := o.attempt(gitSide, append([]string{"stash", "push", "-q"}, tc.args...)...)
			opts := tc.opts
			opts.When = stashOracleWhen()
			_, err := StashPush(t.Context(), o.openRepo(ourSide), opts)
			if (gitErr != nil) != tc.gitFails {
				t.Fatalf("git stash push error = %v, want failure %v", gitErr, tc.gitFails)
			}
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("StashPush returned %v, want %v", err, tc.wantErr)
			}
			requireSameStashState(o, gitSide, ourSide)
		})
	}
}

type stashApplyCase struct {
	name     string
	side     func(*oracle, string) string
	setup    func(*oracle, string)
	push     []string
	prepare  func(*oracle, string)
	pop      bool
	opts     StashApplyOptions
	gitFails bool
	wantErr  error
}

func stashApplyCases() []stashApplyCase {
	return []stashApplyCase{
		{name: "pop untracked", push: []string{"-u"}, pop: true},
		{name: "apply ignored", push: []string{"-a"}},
		{
			name: "untracked file already exists", push: []string{"-u"},
			prepare:  func(o *oracle, dir string) { o.write(dir, "untracked.txt", "other\n") },
			gitFails: true, wantErr: ErrUntrackedNotRestored,
		},
		{
			name: "untracked directory blocked by a file", push: []string{"-u"}, pop: true,
			setup:    func(o *oracle, dir string) { o.write(dir, "fresh/inside.txt", "inside\n") },
			prepare:  func(o *oracle, dir string) { o.write(dir, "fresh", "file\n") },
			gitFails: true, wantErr: ErrUntrackedNotRestored,
		},
		{
			name: "untracked beside a conflict", push: []string{"-u"}, pop: true,
			prepare: func(o *oracle, dir string) {
				o.write(dir, "a.txt", "upstream\n")
				o.run(dir, "commit", "-q", "-am", "upstream")
			},
			gitFails: true,
		},
		{
			name: "untracked beside a blocked merge", push: []string{"-u"},
			prepare:  func(o *oracle, dir string) { o.write(dir, "a.txt", "dirty\n") },
			gitFails: true, wantErr: ErrWouldOverwrite,
		},
		{name: "index", opts: StashApplyOptions{Index: true}},
		{name: "pop index", pop: true, opts: StashApplyOptions{Index: true}},
		{
			name: "index after an unrelated commit", opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				o.write(dir, "e.txt", "e\n")
				o.run(dir, "add", "e.txt")
				o.run(dir, "commit", "-q", "-m", "e")
			},
		},
		{
			name: "index conflicts", pop: true, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				o.write(dir, "b.txt", "upstream\n")
				o.run(dir, "commit", "-q", "-am", "upstream")
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
		{
			name: "index that is already staged", opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) { o.run(dir, "read-tree", "--reset", "-u", "stash^2") },
		},
		{
			name: "index of an unstaged stash", opts: StashApplyOptions{Index: true},
			setup: func(o *oracle, dir string) { o.run(dir, "reset", "-q") },
		},
		{
			name: "index and untracked kept by the push", push: []string{"--keep-index", "-u"}, pop: true, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) { o.run(dir, "reset", "-q", "--hard") },
		},
		{
			name: "index at the first lines refuses shifted lines", side: stashLinesSide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				o.write(dir, "m.txt", "top\n"+numberedLines(1, 12))
				o.run(dir, "commit", "-q", "-am", "top")
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
		{
			name: "index on shifted lines", side: stashLinesSide, opts: StashApplyOptions{Index: true},
			setup: func(o *oracle, dir string) {
				o.write(dir, "m.txt", strings.Replace(numberedLines(1, 12), "l7\n", "X7\n", 1)+"tail\n")
				o.run(dir, "add", "m.txt")
			},
			prepare: func(o *oracle, dir string) {
				o.write(dir, "m.txt", "top\n"+numberedLines(1, 12))
				o.run(dir, "commit", "-q", "-am", "top")
			},
		},
		{
			name: "index with overlapping staged changes", side: stashLinesSide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				stageText(o, dir, "m.txt", strings.Replace(numberedLines(1, 12), "l3\n", "Y3\n", 1))
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
		{
			name: "index with staged changes elsewhere in the file", side: stashLinesSide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				stageText(o, dir, "m.txt", strings.Replace(numberedLines(1, 12), "l9\n", "Y9\n", 1))
			},
			gitFails: true, wantErr: ErrWouldOverwrite,
		},
		{
			name: "index with a staged change in another file", side: stashLinesSide, pop: true, opts: StashApplyOptions{Index: true},
			setup:    func(o *oracle, dir string) { o.write(dir, "loose.txt", "loose\n") },
			push:     []string{"-u"},
			prepare:  func(o *oracle, dir string) { stageText(o, dir, "keep.txt", "staged\n") },
			gitFails: true, wantErr: ErrWouldOverwrite,
		},
		{
			name: "staged changes without restoring the index", side: stashLinesSide,
			prepare: func(o *oracle, dir string) { stageText(o, dir, "keep.txt", "staged\n") },
		},
		{
			name: "binary index", side: stashBinarySide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				stageText(o, dir, "z.txt", "z\n")
				o.run(dir, "commit", "-q", "-m", "z")
			},
		},
		{
			name: "binary index changed since", side: stashBinarySide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				stageText(o, dir, "bin", "bin\x00head\n")
				o.run(dir, "commit", "-q", "-m", "head")
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
		{
			name: "binary made text by attributes in the index", side: stashBinarySide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				markBinaryAsText(o, dir)
				stageText(o, dir, "bin", "bin\x00base\ntail\n")
				o.run(dir, "commit", "-q", "-m", "tail")
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
		{
			name: "symbolic link index", side: stashSymlinkSide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				stageText(o, dir, "z.txt", "z\n")
				o.run(dir, "commit", "-q", "-m", "z")
			},
		},
		{
			name: "symbolic link index changed since", side: stashSymlinkSide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				relink(o, dir, "c.txt")
				o.run(dir, "add", "link")
				o.run(dir, "commit", "-q", "-m", "c")
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
		{
			name: "symbolic link index over a regular file", side: stashSymlinkSide, opts: StashApplyOptions{Index: true},
			prepare: func(o *oracle, dir string) {
				o.remove(dir, "link")
				stageText(o, dir, "link", "a.txt")
				o.run(dir, "commit", "-q", "-m", "regular")
			},
			gitFails: true, wantErr: ErrStashIndexConflicts,
		},
	}
}

func stageText(o *oracle, dir, rel, text string) {
	o.t.Helper()
	o.write(dir, rel, text)
	o.run(dir, "add", rel)
}

func TestOracleStashApplyOptionsMatchGit(t *testing.T) {
	for _, tc := range stashApplyCases() {
		t.Run(tc.name, func(t *testing.T) {
			o := newStashOracle(t)
			side := tc.side
			if side == nil {
				side = stashRichSide
			}
			gitSide, ourSide := side(o, "git"), side(o, "ours")
			for _, dir := range []string{gitSide, ourSide} {
				if tc.setup != nil {
					tc.setup(o, dir)
				}
				o.run(dir, append([]string{"stash", "push", "-q"}, tc.push...)...)
				if tc.prepare != nil {
					tc.prepare(o, dir)
				}
			}
			args := []string{"stash", "apply", "-q"}
			run := StashApply
			if tc.pop {
				args[1], run = "pop", StashPop
			}
			if tc.opts.Index {
				args = append(args, "--index")
			}
			_, gitErr := o.attempt(gitSide, args...)
			result, err := run(t.Context(), o.openRepo(ourSide), 0, tc.opts)
			if (gitErr != nil) != tc.gitFails || (err != nil || !result.Clean()) != tc.gitFails {
				t.Fatalf("git error = %v; ours = %+v, %v; want failure %v", gitErr, result, err, tc.gitFails)
			}
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("apply returned %v, want %v", err, tc.wantErr)
			}
			requireSameStashState(o, gitSide, ourSide)
		})
	}
}

func TestOracleGitAppliesOurStashesWithEveryOption(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		opts StashOptions
	}{
		{"untracked", []string{"-u"}, StashOptions{IncludeUntracked: true}},
		{"keep index", []string{"--keep-index"}, StashOptions{KeepIndex: true}},
		{"staged", []string{"--staged"}, StashOptions{Staged: true}},
		{"paths", []string{"-u", "--", "b.txt", "tools"}, StashOptions{IncludeUntracked: true, Paths: []string{"b.txt", "tools"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newStashOracle(t)
			gitSide, ourSide := stashRichSide(o, "git"), stashRichSide(o, "ours")
			o.run(gitSide, append([]string{"stash", "push", "-q"}, tc.args...)...)
			opts := tc.opts
			opts.When = stashOracleWhen()
			if _, err := StashPush(t.Context(), o.openRepo(ourSide), opts); err != nil {
				t.Fatalf("StashPush returned error %v", err)
			}
			for _, dir := range []string{gitSide, ourSide} {
				o.run(dir, "reset", "-q", "--hard")
				o.run(dir, "stash", "apply", "-q", "--index")
			}
			requireSameStashState(o, gitSide, ourSide)
			o.run(ourSide, "fsck", "--strict")
		})
	}
}

func TestOracleStashShowMatchesGit(t *testing.T) {
	o := newStashOracle(t)
	dir := stashRichSide(o, "show")
	o.run(dir, "stash", "push", "-q", "-u")

	changes, err := StashShow(t.Context(), o.openRepo(dir), 0, diff.Options{})
	if err != nil {
		t.Fatalf("StashShow returned error %v", err)
	}
	for _, part := range []struct {
		files []diff.File
		args  []string
	}{
		{changes.WorkTree, []string{"stash", "show", "-p"}},
		{changes.Index, []string{"diff", "stash^1", "stash^2"}},
		{changes.Untracked, []string{"stash", "show", "-p", "--only-untracked"}},
		{changes.Files(), []string{"stash", "show", "-p", "--include-untracked"}},
	} {
		var got strings.Builder
		for _, file := range part.files {
			if err := diff.Unified(&got, file, diff.Defaults()); err != nil {
				t.Fatalf("Unified returned error %v", err)
			}
		}
		if want := o.run(dir, part.args...); got.String() != want {
			t.Fatalf("git %v:\n%s\nours:\n%s", part.args, want, got.String())
		}
	}
	if changes.Entry.Selector() != "stash@{0}" || changes.Base.String() != strings.TrimSpace(o.run(dir, "rev-parse", "stash^1")) {
		t.Fatalf("StashShow entry = %+v, base %s", changes.Entry, changes.Base)
	}
}
