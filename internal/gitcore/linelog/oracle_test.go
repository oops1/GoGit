//go:build oracle

package linelog

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
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
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=gc.auto",
		"GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=maintenance.auto",
		"GIT_CONFIG_VALUE_1=false",
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
		if content == "" {
			r.git("rm", "-q", "--", rel)
			continue
		}
		r.write(rel, content)
	}
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
}

func (r *repo) move(from, to, message string) {
	r.t.Helper()
	r.clock += 60
	r.git("mv", from, to)
	r.git("commit", "-q", "-m", message)
}

func (r *repo) merge(branch string) {
	r.t.Helper()
	r.clock += 60
	r.git("merge", "-q", "--no-ff", "--no-edit", branch)
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

func numbered(seed string, count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = seed + " line " + strconv.Itoa(i+1)
	}
	return out
}

func joined(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

func replaced(lines []string, at int, with ...string) []string {
	out := append([]string{}, lines[:at]...)
	out = append(out, with...)
	return append(out, lines[at+1:]...)
}

func inserted(lines []string, at int, with ...string) []string {
	out := append([]string{}, lines[:at]...)
	out = append(out, with...)
	return append(out, lines[at:]...)
}

func removed(lines []string, at, count int) []string {
	out := append([]string{}, lines[:at]...)
	return append(out, lines[at+count:]...)
}

func gitLineLog(r *repo, args []string) string {
	r.t.Helper()
	command := []string{"log", "--format=%H", "--no-color"}
	for _, arg := range args {
		command = append(command, "-L", arg)
	}
	return r.git(command...)
}

func ourLineLog(r *repo, args []string, opts Options) (string, error) {
	r.t.Helper()
	var specs []Spec
	for _, arg := range args {
		spec, err := ParseArg(arg)
		if err != nil {
			return "", err
		}
		specs = append(specs, spec)
	}
	var out bytes.Buffer
	for entry, err := range Log(r.t.Context(), r.objects(), r.head(), specs, opts) {
		if err != nil {
			return "", err
		}
		out.WriteString(entry.Commit.ID.String() + "\n")
		if err := entry.WritePatch(&out); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

func compareWithGit(t *testing.T, r *repo, args ...string) {
	t.Helper()
	want := gitLineLog(r, args)
	got, err := ourLineLog(r, args, Options{})
	if err != nil {
		t.Fatalf("Log returned error %v", err)
	}
	if got != want {
		t.Fatalf("line log for %q differs\n--- ours\n%s\n--- git\n%s", args, got, want)
	}
}

type oracleCase struct {
	name  string
	build func(r *repo)
	args  [][]string
}

func oracleCases() []oracleCase {
	base := numbered("f", 30)
	return []oracleCase{
		{name: "simple edits", args: [][]string{{"10,15:f"}, {"1,3:f"}, {"28,30:f"}, {"1,30:f"}, {"12:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base), "other": "x\n"})
			r.commit("edit inside", map[string]string{"f": joined(replaced(base, 11, "EDIT 12"))})
			r.commit("elsewhere", map[string]string{"other": "y\n"})
			r.commit("edit outside", map[string]string{"f": joined(replaced(replaced(base, 11, "EDIT 12"), 25, "EDIT 26"))})
			r.commit("edit edges", map[string]string{"f": joined(replaced(replaced(replaced(base, 11, "EDIT 12"), 25, "EDIT 26"), 9, "EDIT 10"))})
		}},
		{name: "range moved by insertions above", args: [][]string{{"20,24:f"}, {"2,3:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.commit("edit target", map[string]string{"f": joined(replaced(base, 15, "EDIT 16"))})
			moved := inserted(replaced(base, 15, "EDIT 16"), 0, "new 1", "new 2", "new 3", "new 4")
			r.commit("insert above", map[string]string{"f": joined(moved)})
			r.commit("remove above", map[string]string{"f": joined(removed(moved, 5, 2))})
		}},
		{name: "range split and merged", args: [][]string{{"8,20:f"}, {"5,9:f", "12,16:f"}, {"5,10:f", "11,16:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			split := inserted(base, 12, "wedge 1", "wedge 2", "wedge 3")
			r.commit("split", map[string]string{"f": joined(split)})
			r.commit("edit both halves", map[string]string{"f": joined(replaced(replaced(split, 8, "EDIT A"), 17, "EDIT B"))})
			joinedBack := removed(replaced(replaced(split, 8, "EDIT A"), 17, "EDIT B"), 12, 3)
			r.commit("merge halves", map[string]string{"f": joined(joinedBack)})
			r.commit("remove gap", map[string]string{"f": joined(removed(joinedBack, 9, 3))})
		}},
		{name: "renames", args: [][]string{{"5,12:final"}, {"1,30:final"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.commit("edit before move", map[string]string{"f": joined(replaced(base, 6, "EDIT 7"))})
			r.move("f", "middle", "first move")
			r.commit("edit between moves", map[string]string{"middle": joined(replaced(replaced(base, 6, "EDIT 7"), 9, "EDIT 10"))})
			r.commit("rename with edit", map[string]string{"middle": "", "final": joined(replaced(replaced(replaced(base, 6, "EDIT 7"), 9, "EDIT 10"), 10, "EDIT 11"))})
		}},
		{name: "rename competing with an exact copy", args: [][]string{{"3,8:b"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"a": joined(base)})
			r.commit("move", map[string]string{"a": "", "b": joined(replaced(base, 4, "EDIT 5")), "c": joined(base)})
		}},
		{name: "merge where both sides touch the range", args: [][]string{{"5,20:f"}, {"1,4:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.git("branch", "side")
			r.commit("ours", map[string]string{"f": joined(replaced(base, 6, "OURS 7"))})
			r.git("checkout", "-q", "side")
			r.commit("theirs", map[string]string{"f": joined(replaced(base, 16, "THEIRS 17"))})
			r.git("checkout", "-q", "main")
			r.merge("side")
			r.commit("after merge", map[string]string{"f": joined(replaced(replaced(base, 6, "OURS 7"), 16, "AFTER 17"))})
		}},
		{name: "merge where one side touches the range", args: [][]string{{"5,12:f"}, {"20,25:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.git("branch", "side")
			r.commit("ours elsewhere", map[string]string{"f": joined(replaced(base, 22, "OURS 23"))})
			r.git("checkout", "-q", "side")
			r.commit("theirs inside", map[string]string{"f": joined(replaced(base, 8, "THEIRS 9"))})
			r.commit("theirs again", map[string]string{"f": joined(replaced(replaced(base, 8, "THEIRS 9"), 9, "THEIRS 10"))})
			r.git("checkout", "-q", "main")
			r.merge("side")
		}},
		{name: "evil merge", args: [][]string{{"5,12:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.git("branch", "side")
			r.commit("ours", map[string]string{"f": joined(replaced(base, 5, "OURS 6"))})
			r.git("checkout", "-q", "side")
			r.commit("theirs", map[string]string{"f": joined(replaced(base, 10, "THEIRS 11"))})
			r.git("checkout", "-q", "main")
			r.clock += 60
			r.git("merge", "-q", "--no-ff", "--no-commit", "side")
			r.write("f", joined(replaced(replaced(replaced(base, 5, "OURS 6"), 10, "THEIRS 11"), 7, "EVIL 8")))
			r.git("commit", "-q", "-am", "evil merge")
		}},
		{name: "funcname form", args: [][]string{{":beta:code.c"}, {":alpha:code.c"}, {"^:ga.*a:code.c"}, {":\\(gam\\)ma:code.c"}, {":int [a-z]*ta(:code.c"}}, build: func(r *repo) {
			code := []string{"int alpha(void)", "{", "\treturn 1;", "}", "", "int beta(void)", "{", "\treturn 2;", "}", "", "int gamma(void)", "{", "\treturn 3;", "}"}
			r.commit("base", map[string]string{"code.c": joined(code)})
			r.commit("edit beta", map[string]string{"code.c": joined(replaced(code, 7, "\treturn 22;"))})
			r.commit("edit gamma", map[string]string{"code.c": joined(replaced(replaced(code, 7, "\treturn 22;"), 12, "\treturn 33;"))})
		}},
		{name: "regex and offset ranges", args: [][]string{{"/line 10$/,+4:f"}, {"/line 20/,-3:f"}, {"12,/line 19/:f"}, {"5,8:f", "/line 1/,+2:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.commit("edit", map[string]string{"f": joined(replaced(replaced(base, 11, "EDIT 12"), 17, "EDIT 18"))})
		}},
		{name: "file created within the range", args: [][]string{{"1,12:f"}, {"4,6:f"}}, build: func(r *repo) {
			r.commit("unrelated", map[string]string{"other": "x\n"})
			r.commit("create", map[string]string{"f": joined(base[:5])})
			r.commit("grow", map[string]string{"f": joined(base[:12])})
			r.commit("edit", map[string]string{"f": joined(replaced(base[:12], 4, "EDIT 5"))})
		}},
		{name: "file recreated after deletion", args: [][]string{{"2,6:f"}}, build: func(r *repo) {
			r.commit("create", map[string]string{"f": joined(base[:10])})
			r.commit("delete", map[string]string{"f": ""})
			r.commit("recreate", map[string]string{"f": joined(replaced(base[:10], 3, "BACK 4"))})
		}},
		{name: "crlf file", args: [][]string{{"3,7:f"}}, build: func(r *repo) {
			crlf := func(lines []string) string { return strings.Join(lines, "\r\n") + "\r\n" }
			r.commit("base", map[string]string{"f": crlf(base[:10])})
			r.commit("edit", map[string]string{"f": crlf(replaced(base[:10], 4, "EDIT 5"))})
			r.commit("to lf", map[string]string{"f": joined(replaced(base[:10], 4, "EDIT 5"))})
		}},
		{name: "missing final newline", args: [][]string{{"8,10:f"}, {"10:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": strings.Join(base[:10], "\n")})
			r.commit("edit last", map[string]string{"f": strings.Join(replaced(base[:10], 9, "LAST"), "\n")})
			r.commit("add newline", map[string]string{"f": joined(replaced(base[:10], 9, "LAST"))})
		}},
		{name: "two files", args: [][]string{{"2,5:b", "3,6:a"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"a": joined(base[:10]), "b": joined(numbered("b", 10))})
			r.commit("edit both", map[string]string{"a": joined(replaced(base[:10], 4, "EDIT A")), "b": joined(replaced(numbered("b", 10), 2, "EDIT B"))})
			r.commit("edit a", map[string]string{"a": joined(replaced(replaced(base[:10], 4, "EDIT A"), 5, "EDIT A2"))})
		}},
		{name: "deleted lines inside the range", args: [][]string{{"5,15:f"}}, build: func(r *repo) {
			r.commit("base", map[string]string{"f": joined(base)})
			r.commit("delete inside", map[string]string{"f": joined(removed(base, 8, 3))})
			r.commit("delete at edge", map[string]string{"f": joined(removed(removed(base, 8, 3), 4, 1))})
		}},
	}
}

func TestOracleLineLogMatchesGit(t *testing.T) {
	for _, c := range oracleCases() {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			c.build(r)
			for _, args := range c.args {
				compareWithGit(t, r, args...)
			}
		})
	}
}

func TestOracleLineLogThroughAGitCommitGraphMatchesGit(t *testing.T) {
	for _, c := range oracleCases() {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			c.build(r)
			r.git("commit-graph", "write", "--reachable", "--changed-paths")
			graph, err := commitgraph.Open([]string{filepath.Join(r.dir, ".git", "objects")}, commitgraph.OpenOptions{})
			if err != nil || graph == nil {
				t.Fatalf("commitgraph.Open returned %v, %v", graph, err)
			}
			for _, args := range c.args {
				want := gitLineLog(r, args)
				got, err := ourLineLog(r, args, Options{Graph: graph})
				if err != nil || got != want {
					t.Fatalf("line log for %q differs (%v)\n--- ours\n%s\n--- git\n%s", args, err, got, want)
				}
			}
		})
	}
}

func TestOracleLineLogWithoutRenamesMatchesGit(t *testing.T) {
	for _, c := range oracleCases() {
		if !strings.Contains(c.name, "rename") {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			c.build(r)
			for _, args := range c.args {
				command := []string{"log", "--format=%H", "--no-color", "--no-renames"}
				for _, arg := range args {
					command = append(command, "-L", arg)
				}
				want := r.git(command...)
				got, err := ourLineLog(r, args, Options{NoRenames: true})
				if err != nil || got != want {
					t.Fatalf("line log for %q differs (%v)\n--- ours\n%s\n--- git\n%s", args, err, got, want)
				}
			}
		})
	}
}

func TestOracleLineLogMatchesGitAcrossRenamedFilesAndMerges(t *testing.T) {
	r := newRepo(t)
	base := numbered("f", 20)
	other := numbered("g", 20)
	r.commit("base", map[string]string{"f": joined(base), "g": joined(other)})
	r.git("branch", "side")
	r.commit("edit f", map[string]string{"f": joined(replaced(base, 3, "MAIN 4"))})
	r.move("g", "h", "move g")
	r.git("checkout", "-q", "side")
	r.commit("edit g on side", map[string]string{"g": joined(replaced(other, 5, "SIDE 6"))})
	r.commit("edit f on side", map[string]string{"f": joined(replaced(base, 12, "SIDE 13"))})
	r.git("checkout", "-q", "main")
	r.merge("side")
	r.commit("after", map[string]string{"h": joined(replaced(replaced(other, 5, "SIDE 6"), 7, "AFTER 8")), "f": joined(replaced(replaced(base, 3, "MAIN 4"), 12, "AFTER 13"))})

	compareWithGit(t, r, "2,15:f", "4,9:h")
	compareWithGit(t, r, "1,20:h", "10,14:f")
}

func TestOracleLineLogMatchesGitOnRandomHistories(t *testing.T) {
	for seed := range uint64(6) {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, 99))
			r := newRepo(t)
			lines := numbered("f", 40)
			r.commit("base", map[string]string{"f": joined(lines)})
			branch := 0
			for step := range 30 {
				if step%10 == 9 {
					branch++
					name := "b" + strconv.Itoa(branch)
					r.git("checkout", "-q", "-b", name, "HEAD~2")
					side := randomEdit(rng, randomEdit(rng, readLines(r)))
					r.commit("side "+strconv.Itoa(step), map[string]string{"g": joined(side)})
					r.git("checkout", "-q", "main")
					r.merge(name)
					continue
				}
				lines = randomEdit(rng, readLines(r))
				r.commit("step "+strconv.Itoa(step), map[string]string{"f": joined(lines)})
			}
			total := len(readLines(r))
			for range 8 {
				first := 1 + rng.IntN(total)
				last := min(total, first+rng.IntN(8))
				compareWithGit(t, r, fmt.Sprintf("%d,%d:f", first, last))
			}
		})
	}
}

func readLines(r *repo) []string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "f"))
	if err != nil {
		r.t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func randomEdit(rng *rand.Rand, lines []string) []string {
	at := rng.IntN(len(lines))
	switch rng.IntN(4) {
	case 0:
		return replaced(lines, at, "edit "+strconv.Itoa(rng.IntN(1000)))
	case 1:
		return inserted(lines, at, "new "+strconv.Itoa(rng.IntN(1000)), "new "+strconv.Itoa(rng.IntN(1000)))
	case 2:
		if len(lines) > 10 {
			return removed(lines, at, min(2, len(lines)-at))
		}
		return lines
	default:
		return inserted(replaced(lines, at, "swap "+strconv.Itoa(rng.IntN(1000))), 0, "top "+strconv.Itoa(rng.IntN(1000)))
	}
}
