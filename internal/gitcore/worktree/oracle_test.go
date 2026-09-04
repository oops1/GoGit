//go:build oracle

package worktree

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type oracleEnv map[string]string

func (e oracleEnv) get(key string) string { return e[key] }

type oracle struct {
	t    *testing.T
	home string
	env  []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	globalConfig := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return &oracle{
		t:    t,
		home: home,
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_GLOBAL=" + globalConfig,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
		},
	}
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	out, err := o.attempt(dir, args...)
	if err != nil {
		o.t.Fatalf("%v", err)
	}
	return out
}

func (o *oracle) attempt(dir string, args ...string) (string, error) {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, errBuf.String())
	}
	return out.String(), err
}

func (o *oracle) repoDir(name string) string {
	dir := filepath.Join(o.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	return dir
}

func (o *oracle) options() repo.OpenOptions {
	return repo.OpenOptions{Env: oracleEnv{}.get, NoSystem: true, GlobalFile: filepath.Join(o.home, "gitconfig")}
}

func (o *oracle) write(dir, rel, text string) {
	o.t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(full, []byte(text), 0o666); err != nil {
		o.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (o *oracle) status(dir string) (changed map[string]porcelainEntry, untracked map[string]bool) {
	o.t.Helper()
	out := o.run(dir, "status", "--porcelain=v2", "-z", "--untracked-files=normal")
	return parsePorcelainV2(o.t, out)
}

func (o *oracle) ourStatus(dir string) Status {
	o.t.Helper()
	r, err := repo.Open(dir, o.options())
	if err != nil {
		o.t.Fatalf("repo.Open returned error %v", err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		o.t.Fatalf("odb.Open returned error %v", err)
	}
	defer func() { _ = db.Close() }()
	refsStore, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		o.t.Fatalf("refs.Open returned error %v", err)
	}
	defer func() { _ = refsStore.Close() }()
	w, err := Open(r, Options{DB: db, Refs: refsStore, Env: oracleEnv{}.get})
	if err != nil {
		o.t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = w.Close() }()
	status, err := w.Status(o.t.Context())
	if err != nil {
		o.t.Fatalf("Status returned error %v", err)
	}
	return status
}

type porcelainEntry struct {
	x, y     byte
	origPath string
}

func parsePorcelainV2(t *testing.T, out string) (changed map[string]porcelainEntry, untracked map[string]bool) {
	t.Helper()
	changed = map[string]porcelainEntry{}
	untracked = map[string]bool{}
	fields := strings.Split(out, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if record == "" {
			continue
		}
		switch record[0] {
		case '1':
			parts := strings.SplitN(record, " ", 9)
			if len(parts) != 9 {
				t.Fatalf("unexpected ordinary record %q", record)
			}
			changed[parts[8]] = porcelainEntry{x: parts[1][0], y: parts[1][1]}
		case '2':
			parts := strings.SplitN(record, " ", 10)
			if len(parts) != 10 {
				t.Fatalf("unexpected rename record %q", record)
			}
			i++
			if i >= len(fields) {
				t.Fatalf("rename record %q is missing its origPath field", record)
			}
			changed[parts[9]] = porcelainEntry{x: parts[1][0], y: parts[1][1], origPath: fields[i]}
		case 'u':
			parts := strings.SplitN(record, " ", 11)
			if len(parts) != 11 {
				t.Fatalf("unexpected unmerged record %q", record)
			}
			changed[parts[10]] = porcelainEntry{x: parts[1][0], y: parts[1][1]}
		case '?':
			untracked[record[2:]] = true
		case '!':
		default:
			t.Fatalf("unexpected record %q", record)
		}
	}
	return changed, untracked
}

var conflictXY = map[ConflictKind][2]byte{
	ConflictBothDeleted:   {'D', 'D'},
	ConflictAddedByUs:     {'A', 'U'},
	ConflictDeletedByThem: {'U', 'D'},
	ConflictAddedByThem:   {'U', 'A'},
	ConflictDeletedByUs:   {'D', 'U'},
	ConflictBothAdded:     {'A', 'A'},
	ConflictBothModified:  {'U', 'U'},
}

func translateCode(code StatusCode) byte {
	if code == StatusUnmodified {
		return '.'
	}
	return byte(code)
}

func compareStatus(t *testing.T, ours Status, wantChanged map[string]porcelainEntry, wantUntracked map[string]bool) {
	t.Helper()
	remainingChanged := make(map[string]porcelainEntry, len(wantChanged))
	for path, entry := range wantChanged {
		remainingChanged[path] = entry
	}
	remainingUntracked := make(map[string]bool, len(wantUntracked))
	for path := range wantUntracked {
		remainingUntracked[path] = true
	}

	for _, entry := range ours.Entries {
		if entry.Unstaged == StatusIgnored && entry.Staged == StatusUnmodified && entry.Conflict == ConflictNone {
			continue
		}
		if entry.Unstaged == StatusUntracked && entry.Staged == StatusUnmodified && entry.Conflict == ConflictNone {
			if !wantUntracked[entry.Path] {
				t.Errorf("we report %q as untracked, git does not", entry.Path)
			}
			delete(remainingUntracked, entry.Path)
			continue
		}
		var x, y byte
		if entry.Conflict != ConflictNone {
			pair, ok := conflictXY[entry.Conflict]
			if !ok {
				t.Fatalf("unmapped conflict kind %v for %q", entry.Conflict, entry.Path)
			}
			x, y = pair[0], pair[1]
		} else {
			x, y = translateCode(entry.Staged), translateCode(entry.Unstaged)
		}
		want, ok := wantChanged[entry.Path]
		if !ok {
			t.Errorf("we report %q as %c%c, git does not report it as changed", entry.Path, x, y)
			continue
		}
		if want.x != x || want.y != y {
			t.Errorf("%s: our XY = %c%c, git XY = %c%c", entry.Path, x, y, want.x, want.y)
		}
		if x == 'R' && want.origPath != entry.OrigPath {
			t.Errorf("%s: our OrigPath = %q, git OrigPath = %q", entry.Path, entry.OrigPath, want.origPath)
		}
		delete(remainingChanged, entry.Path)
	}
	for path, entry := range remainingChanged {
		t.Errorf("git reports %q as %c%c, we do not report it", path, entry.x, entry.y)
	}
	for path := range remainingUntracked {
		t.Errorf("git reports %q as untracked, we do not report it", path)
	}
}

func (o *oracle) compare(dir string) {
	o.t.Helper()
	wantChanged, wantUntracked := o.status(dir)
	ours := o.ourStatus(dir)
	compareStatus(o.t, ours, wantChanged, wantUntracked)
}

func TestOracleStatusMatchesGitStatusPorcelainV2(t *testing.T) {
	tests := []struct {
		name  string
		setup func(o *oracle, dir string)
	}{
		{"clean worktree after commit", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.write(dir, "sub/b.txt", "world\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
		}},
		{"unstaged modification", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "a.txt", "goodbye\n")
		}},
		{"staged modification via git add", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "a.txt", "goodbye\n")
			o.run(dir, "add", "a.txt")
		}},
		{"added file via git add", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "new.txt", "new\n")
			o.run(dir, "add", "new.txt")
		}},
		{"deleted file via git rm", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.run(dir, "rm", "-q", "a.txt")
		}},
		{"deleted file from the worktree without git rm", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
				o.t.Fatalf("Remove returned error %v", err)
			}
		}},
		{"renamed file via git mv", func(o *oracle, dir string) {
			o.write(dir, "old.txt", "same content\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.run(dir, "mv", "old.txt", "new.txt")
		}},
		{"untracked file", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "untracked.txt", "new\n")
		}},
		{"untracked directory collapses to one entry", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "newdir/one.txt", "1\n")
			o.write(dir, "newdir/nested/two.txt", "2\n")
		}},
		{"gitignore hides matching files", func(o *oracle, dir string) {
			o.write(dir, ".gitignore", "*.log\nbuild/\n")
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "debug.log", "noise\n")
			o.write(dir, "build/output.bin", "binary\n")
		}},
		{"executable bit change", func(o *oracle, dir string) {
			o.write(dir, "run.sh", "echo hi\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o755); err != nil {
				o.t.Fatalf("Chmod returned error %v", err)
			}
		}},
		{"empty repository", func(o *oracle, dir string) {}},
		{"unborn head with staged files", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", "a.txt")
		}},
		{"conflict from a failed merge", func(o *oracle, dir string) {
			o.write(dir, "shared.txt", "base\n")
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "base")
			o.run(dir, "checkout", "-q", "-b", "topic")
			o.write(dir, "shared.txt", "topic change\n")
			o.run(dir, "commit", "-q", "-a", "-m", "topic change")
			o.run(dir, "checkout", "-q", "main")
			o.write(dir, "shared.txt", "main change\n")
			o.run(dir, "commit", "-q", "-a", "-m", "main change")
			_, _ = o.attempt(dir, "merge", "topic", "-q", "--no-edit")
		}},
		{"conflict where we deleted and they modified", func(o *oracle, dir string) {
			o.write(dir, "shared.txt", "base\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "base")
			o.run(dir, "checkout", "-q", "-b", "topic")
			o.write(dir, "shared.txt", "topic change\n")
			o.run(dir, "commit", "-q", "-a", "-m", "topic change")
			o.run(dir, "checkout", "-q", "main")
			o.run(dir, "rm", "-q", "shared.txt")
			o.run(dir, "commit", "-q", "-m", "delete on main")
			_, _ = o.attempt(dir, "merge", "topic", "-q", "--no-edit")
		}},
		{"conflict where both sides added the same path", func(o *oracle, dir string) {
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "base")
			o.run(dir, "checkout", "-q", "-b", "topic")
			o.write(dir, "shared.txt", "from topic\n")
			o.run(dir, "add", "shared.txt")
			o.run(dir, "commit", "-q", "-m", "topic add")
			o.run(dir, "checkout", "-q", "main")
			o.write(dir, "shared.txt", "from main\n")
			o.run(dir, "add", "shared.txt")
			o.run(dir, "commit", "-q", "-m", "main add")
			_, _ = o.attempt(dir, "merge", "topic", "-q", "--no-edit")
		}},
		{"type change from file to symlink", func(o *oracle, dir string) {
			o.write(dir, "thing.txt", "content\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			if err := os.Remove(filepath.Join(dir, "thing.txt")); err != nil {
				o.t.Fatalf("Remove returned error %v", err)
			}
			if err := os.Symlink("target.txt", filepath.Join(dir, "thing.txt")); err != nil {
				o.t.Skipf("cannot create symlinks on this platform: %v", err)
			}
		}},
		{"nested tracked and untracked files coexist", func(o *oracle, dir string) {
			o.write(dir, "dir/tracked.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "dir/untracked.txt", "new\n")
		}},
		{"crlf file normalized by text=auto", func(o *oracle, dir string) {
			o.write(dir, ".gitattributes", "*.txt text=auto\n")
			o.write(dir, "crlf.txt", "line1\r\nline2\r\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newOracle(t)
			dir := o.repoDir("work")
			o.run(dir, "init", "-q", "-b", "main", ".")
			o.run(dir, "config", "core.autocrlf", "false")
			tc.setup(o, dir)
			o.compare(dir)
		})
	}
}

type ignoredPorcelain struct {
	changed   map[string]porcelainEntry
	untracked map[string]bool
	ignored   map[string]bool
}

func parsePorcelainV2WithIgnored(t *testing.T, out string) ignoredPorcelain {
	t.Helper()
	changed, untracked := parsePorcelainV2(t, out)
	result := ignoredPorcelain{changed: changed, untracked: untracked, ignored: map[string]bool{}}
	fields := strings.Split(out, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	for _, record := range fields {
		if strings.HasPrefix(record, "! ") {
			result.ignored[record[2:]] = true
		}
	}
	return result
}

func (o *oracle) statusIgnored(dir string) ignoredPorcelain {
	o.t.Helper()
	out := o.run(dir, "status", "--porcelain=v2", "-z", "--ignored=traditional")
	return parsePorcelainV2WithIgnored(o.t, out)
}

func compareStatusWithIgnored(t *testing.T, ours Status, want ignoredPorcelain) {
	t.Helper()
	remainingChanged := make(map[string]porcelainEntry, len(want.changed))
	for path, entry := range want.changed {
		remainingChanged[path] = entry
	}
	remainingUntracked := make(map[string]bool, len(want.untracked))
	for path := range want.untracked {
		remainingUntracked[path] = true
	}
	remainingIgnored := make(map[string]bool, len(want.ignored))
	for path := range want.ignored {
		remainingIgnored[path] = true
	}

	for _, entry := range ours.Entries {
		switch {
		case entry.Staged == StatusUnmodified && entry.Unstaged == StatusIgnored && entry.Conflict == ConflictNone:
			if !want.ignored[entry.Path] {
				t.Errorf("we report %q as ignored, git does not", entry.Path)
			}
			delete(remainingIgnored, entry.Path)
		case entry.Unstaged == StatusUntracked && entry.Staged == StatusUnmodified && entry.Conflict == ConflictNone:
			if !want.untracked[entry.Path] {
				t.Errorf("we report %q as untracked, git does not", entry.Path)
			}
			delete(remainingUntracked, entry.Path)
		default:
			x, y := translateCode(entry.Staged), translateCode(entry.Unstaged)
			wantEntry, ok := want.changed[entry.Path]
			if !ok {
				t.Errorf("we report %q as %c%c, git does not report it as changed", entry.Path, x, y)
				continue
			}
			if wantEntry.x != x || wantEntry.y != y {
				t.Errorf("%s: our XY = %c%c, git XY = %c%c", entry.Path, x, y, wantEntry.x, wantEntry.y)
			}
			delete(remainingChanged, entry.Path)
		}
	}
	for path, entry := range remainingChanged {
		t.Errorf("git reports %q as %c%c, we do not report it", path, entry.x, entry.y)
	}
	for path := range remainingUntracked {
		t.Errorf("git reports %q as untracked, we do not report it", path)
	}
	for path := range remainingIgnored {
		t.Errorf("git reports %q as ignored, we do not report it", path)
	}
}

func (o *oracle) compareIgnored(dir string) {
	o.t.Helper()
	want := o.statusIgnored(dir)
	ours := o.ourStatus(dir)
	compareStatusWithIgnored(o.t, ours, want)
}

func TestOracleIgnoredEntriesMatchGitStatusPorcelainIgnoredTraditional(t *testing.T) {
	tests := []struct {
		name  string
		setup func(o *oracle, dir string)
	}{
		{"ignored directory without tracked files collapses to one entry", func(o *oracle, dir string) {
			o.write(dir, ".gitignore", "ignored/\n")
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "ignored/one.txt", "content\n")
			o.write(dir, "ignored/nested/two.txt", "content\n")
		}},
		{"ignored directory holding a force-tracked file is walked into", func(o *oracle, dir string) {
			o.write(dir, ".gitignore", "ignored/\n")
			o.write(dir, "ignored/tracked.txt", "hello\n")
			o.run(dir, "add", "-f", "ignored/tracked.txt")
			o.run(dir, "add", ".gitignore")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "ignored/tracked.txt", "changed\n")
			o.write(dir, "ignored/untracked.txt", "content\n")
		}},
		{"ignored file sits next to a modified tracked file", func(o *oracle, dir string) {
			o.write(dir, ".gitignore", "*.log\n")
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "a.txt", "goodbye\n")
			o.write(dir, "debug.log", "noise\n")
		}},
		{"a directory holding only ignored files collapses as ignored", func(o *oracle, dir string) {
			o.write(dir, ".gitignore", "*.log\n")
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "mixed/notes.log", "content\n")
			o.write(dir, "mixed/deep/more.log", "content\n")
		}},
		{"an untracked file next to an ignored directory", func(o *oracle, dir string) {
			o.write(dir, ".gitignore", "build/\n")
			o.write(dir, "a.txt", "hello\n")
			o.run(dir, "add", ".")
			o.run(dir, "commit", "-q", "-m", "initial")
			o.write(dir, "build/output.bin", "binary\n")
			o.write(dir, "scratch/notes.txt", "todo\n")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newOracle(t)
			dir := o.repoDir("work")
			o.run(dir, "init", "-q", "-b", "main", ".")
			o.run(dir, "config", "core.autocrlf", "false")
			tc.setup(o, dir)
			o.compareIgnored(dir)
		})
	}
}
