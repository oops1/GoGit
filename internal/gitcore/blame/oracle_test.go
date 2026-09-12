//go:build oracle

package blame

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

type repo struct {
	t     *testing.T
	dir   string
	clock int64
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	r := &repo{t: t, dir: t.TempDir(), clock: 1700000000}
	r.git("init", "-q", "-b", "main", ".")
	r.git("config", "user.name", "oracle")
	r.git("config", "user.email", "oracle@example.com")
	r.git("config", "core.autocrlf", "false")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	stamp := strconv.FormatInt(r.clock, 10) + " +0000"
	cmd := exec.CommandContext(r.t.Context(), "git", args...)
	cmd.Dir = r.dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"HOME=" + r.dir,
		"USERPROFILE=" + r.dir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(r.dir, ".no-global"),
		"GIT_AUTHOR_DATE=" + stamp,
		"GIT_COMMITTER_DATE=" + stamp,
	}
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, errs.String())
	}
	return out.String()
}

func (r *repo) write(rel, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o666); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(message string, files map[string]string) {
	r.t.Helper()
	r.clock += 60
	for rel, content := range files {
		r.write(rel, content)
	}
	r.git("add", "-A")
	r.git("commit", "-q", "-m", message)
}

func (r *repo) head() hash.ObjectID {
	r.t.Helper()
	id, err := hash.Parse(strings.TrimSpace(r.git("rev-parse", "HEAD")))
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *repo) objects() *odb.DB {
	r.t.Helper()
	db, err := odb.Open(filepath.Join(r.dir, ".git", "objects"), odb.Options{})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = db.Close() })
	return db
}

func gitBlame(t *testing.T, r *repo, path string, follow bool) []string {
	t.Helper()
	args := []string{"blame", "--porcelain"}
	if !follow {
		args = append(args, "--no-follow")
	}
	args = append(args, "HEAD", "--", path)
	var out []string
	for block := range strings.SplitSeq(r.git(args...), "\n") {
		fields := strings.Fields(block)
		if len(fields) >= 3 && len(fields[0]) == 40 {
			out = append(out, fields[0]+" "+fields[1]+" "+fields[2])
		}
	}
	return out
}

func ourBlame(t *testing.T, r *repo, path string, follow bool) []string {
	t.Helper()
	result, err := File(t.Context(), r.objects(), r.head(), path, Options{FollowRenames: follow})
	if err != nil {
		t.Fatalf("File returned error %v", err)
	}
	var out []string
	for _, line := range result.Lines {
		out = append(out, line.Commit.String()+" "+strconv.Itoa(line.Source)+" "+strconv.Itoa(line.Number))
	}
	return out
}

func lines(seed string, count int) string {
	var out []string
	for i := range count {
		out = append(out, seed+" line "+strconv.Itoa(i))
	}
	return strings.Join(out, "\n") + "\n"
}

func editLine(text string, line int, replacement string) string {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	parts[line] = replacement
	return strings.Join(parts, "\n") + "\n"
}

type blameCase struct {
	name   string
	build  func(r *repo)
	path   string
	follow bool
}

func blameCases() []blameCase {
	f := lines("f", 10)
	return []blameCase{
		{name: "one commit", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
		}},
		{name: "a later edit", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.commit("edit", map[string]string{"f": editLine(f, 4, "EDITED")})
		}},
		{name: "edits all over", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.commit("first", map[string]string{"f": editLine(f, 0, "FIRST")})
			r.commit("second", map[string]string{"f": editLine(editLine(f, 0, "FIRST"), 9, "LAST")})
		}},
		{name: "lines added in the middle", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.commit("insert", map[string]string{"f": strings.Replace(f, "f line 5\n", "f line 5\nINSERTED\nALSO\n", 1)})
		}},
		{name: "lines removed", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.commit("shrink", map[string]string{"f": strings.Replace(f, "f line 3\nf line 4\n", "", 1)})
		}},
		{name: "a file rewritten wholesale", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.commit("rewrite", map[string]string{"f": lines("g", 4)})
		}},
		{name: "a merge of two sides", path: "f", build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.git("branch", "feature")
			r.commit("ours", map[string]string{"f": editLine(f, 0, "OURS")})
			r.git("checkout", "-q", "feature")
			r.commit("theirs", map[string]string{"f": editLine(f, 9, "THEIRS")})
			r.git("checkout", "-q", "main")
			r.clock += 60
			r.git("merge", "--no-edit", "feature")
		}},
		{name: "a file in a directory", path: "dir/f", build: func(r *repo) {
			r.commit("base", map[string]string{"dir/f": f})
			r.commit("edit", map[string]string{"dir/f": editLine(f, 2, "EDITED")})
		}},
		{name: "a renamed file", path: "moved", follow: true, build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.git("mv", "f", "moved")
			r.clock += 60
			r.git("commit", "-q", "-m", "move")
			r.commit("edit", map[string]string{"moved": editLine(f, 6, "EDITED")})
		}},
		{name: "a rename that also changed the file", path: "moved", follow: true, build: func(r *repo) {
			r.commit("base", map[string]string{"f": f})
			r.git("rm", "-q", "f")
			r.write("moved", editLine(f, 1, "CHANGED WHILE MOVING"))
			r.clock += 60
			r.git("add", "-A")
			r.git("commit", "-q", "-m", "move and edit")
		}},
	}
}

func TestOracleBlameMatchesGitBlame(t *testing.T) {
	for _, c := range blameCases() {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			c.build(r)

			got, want := ourBlame(t, r, c.path, c.follow), gitBlame(t, r, c.path, c.follow)

			if len(got) != len(want) {
				t.Fatalf("blamed %d lines, git blamed %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("line %d: %s, git says %s", i+1, got[i], want[i])
				}
			}
		})
	}
}
