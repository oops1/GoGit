//go:build oracle

package revision

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

type oracle struct {
	t     *testing.T
	root  string
	repo  string
	home  string
	clock int64
	ctx   Context
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	o := &oracle{t: t, root: root, repo: filepath.Join(root, "repo"), home: home, clock: 1700000000}
	o.run(root, "init", "-q", "-b", "main", "repo")
	o.git("config", "user.name", "oracle")
	o.git("config", "user.email", "oracle@example.com")
	o.git("config", "gc.auto", "0")
	return o
}

func (o *oracle) env() []string {
	stamp := strconv.FormatInt(o.clock, 10) + " +0000"
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"HOME=" + o.home,
		"USERPROFILE=" + o.home,
		"XDG_CONFIG_HOME=" + filepath.Join(o.home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=oracle",
		"GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_AUTHOR_DATE=" + stamp,
		"GIT_COMMITTER_NAME=oracle",
		"GIT_COMMITTER_EMAIL=oracle@example.com",
		"GIT_COMMITTER_DATE=" + stamp,
	}
}

func (o *oracle) command(dir string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env()
	return cmd
}

func (o *oracle) succeeds(args ...string) bool {
	o.t.Helper()
	return o.command(o.repo, args).Run() == nil
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	out, err := o.command(dir, args).Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func (o *oracle) git(args ...string) string {
	o.t.Helper()
	return o.run(o.repo, args...)
}

func (o *oracle) lines(args ...string) []string {
	o.t.Helper()
	out := strings.TrimSpace(o.git(args...))
	if out == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
}

func (o *oracle) linesIn(dir string, args ...string) []string {
	o.t.Helper()
	out := strings.TrimSpace(o.run(dir, args...))
	if out == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
}

func (o *oracle) parse(spec string) hash.ObjectID {
	o.t.Helper()
	id, err := hash.Parse(strings.TrimSpace(o.git("rev-parse", spec)))
	if err != nil {
		o.t.Fatalf("git rev-parse %s returned an unusable id: %v", spec, err)
	}
	return id
}

func (o *oracle) commit(message string, files map[string]string) {
	o.t.Helper()
	for _, name := range slices.Sorted(fileNames(files)) {
		path := filepath.Join(o.repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			o.t.Fatalf("MkdirAll returned error %v", err)
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			o.t.Fatalf("WriteFile returned error %v", err)
		}
	}
	o.clock += 60
	o.git("add", "-A")
	o.git("commit", "-q", "-m", message)
}

func regexpMustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func fileNames(files map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for name := range files {
			if !yield(name) {
				return
			}
		}
	}
}

func (o *oracle) merge(branch, message string) {
	o.t.Helper()
	o.clock += 60
	o.git("merge", "-q", "--no-ff", "-m", message, branch)
}

func (o *oracle) checkout(args ...string) {
	o.t.Helper()
	o.clock += 60
	o.git(append([]string{"checkout", "-q"}, args...)...)
}

func (o *oracle) open() Context {
	o.t.Helper()
	gitDir := filepath.Join(o.repo, ".git")
	store, err := refs.Open(refs.Options{GitDir: gitDir})
	if err != nil {
		o.t.Fatalf("refs.Open returned error %v", err)
	}
	o.t.Cleanup(func() { _ = store.Close() })
	o.ctx = Context{
		Objects: looseObjects{dir: filepath.Join(gitDir, "objects")},
		Refs:    store,
		Config:  fakeConfig{upstream: map[string]string{"main": "refs/remotes/origin/main"}},
	}
	return o.ctx
}

func buildOracleRepository(t *testing.T) *oracle {
	t.Helper()
	o := newOracle(t)
	o.commit("first commit", map[string]string{"file.txt": "one", "dir/nested.txt": "n1"})
	o.commit("second commit", map[string]string{"file.txt": "two"})
	o.git("tag", "v1")
	o.commit("third commit", map[string]string{"file.txt": "three"})
	o.checkout("-b", "topic", "HEAD~1")
	o.commit("topic work", map[string]string{"topic.txt": "t1"})
	o.commit("topic follow up", map[string]string{"dir/nested.txt": "n2"})
	o.clock += 60
	o.git("tag", "-a", "-m", "annotated release", "v2")
	o.checkout("main")
	o.commit("main work", map[string]string{"main.txt": "m1"})
	o.merge("topic", "merge topic into main")
	o.checkout("-b", "side", "HEAD~1")
	o.commit("side work", map[string]string{"side.txt": "s1"})
	o.checkout("main")
	o.merge("side", "merge side into main")
	o.git("update-ref", "refs/remotes/origin/main", o.parse("main~2").String())
	o.git("config", "remote.origin.url", "https://example.com/repo.git")
	o.git("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	o.git("config", "branch.main.remote", "origin")
	o.git("config", "branch.main.merge", "refs/heads/main")
	return o
}

func TestOracleParseMatchesRevParse(t *testing.T) {
	o := buildOracleRepository(t)
	ctx := o.open()
	full := o.parse("main~3").String()
	specs := []string{
		"HEAD",
		"@",
		"main",
		"refs/heads/main",
		"topic",
		"side",
		"v1",
		"v2",
		"v2^{}",
		"v2^{commit}",
		"v2^{tag}",
		"v2^{object}",
		"v2^{tree}",
		"main^{tree}",
		"main^{commit}",
		"HEAD^",
		"HEAD^1",
		"HEAD^2",
		"HEAD^0",
		"HEAD~0",
		"HEAD~1",
		"HEAD~3",
		"HEAD^^",
		"main~~",
		"HEAD^2~1",
		"v2~1",
		"v1^",
		"origin/main",
		"HEAD@{0}",
		"HEAD@{1}",
		"HEAD@{3}",
		"main@{0}",
		"main@{1}",
		"@{0}",
		"@{1}",
		"@{-1}",
		"@{-2}",
		"@{-1}~1",
		"main@{upstream}",
		"main@{u}",
		"@{u}",
		"@{upstream}",
		":/topic work",
		"HEAD^{/side work}",
		"HEAD:file.txt",
		"HEAD:dir",
		"HEAD:dir/nested.txt",
		"HEAD:",
		"v2:topic.txt",
		"main^{tree}",
		full,
		full[:8],
		full[:12],
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			want := o.parse(spec)
			rev, err := Parse(spec, ctx)
			if err != nil {
				t.Fatalf("Parse(%q) returned error %v, git resolved it to %s", spec, err, want)
			}
			if rev.ID != want {
				t.Errorf("Parse(%q) resolved to %s, git resolved it to %s", spec, rev.ID, want)
			}
		})
	}
}

func TestOracleWalkMatchesRevList(t *testing.T) {
	o := buildOracleRepository(t)
	ctx := o.open()
	tests := []struct {
		name  string
		args  []string
		specs []string
		setup func(*Options)
	}{
		{"default order", []string{"HEAD"}, []string{"HEAD"}, nil},
		{"all references", []string{"--all"}, []string{"--all"}, nil},
		{
			"topological order",
			[]string{"--topo-order", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Order = Topo },
		},
		{
			"date order",
			[]string{"--date-order", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Order = DateOrder },
		},
		{
			"author date order",
			[]string{"--author-date-order", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Order = AuthorDate },
		},
		{
			"reverse",
			[]string{"--reverse", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Reverse = true },
		},
		{
			"first parent",
			[]string{"--first-parent", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.FirstParent = true },
		},
		{"two dot range", []string{"topic..main"}, []string{"topic..main"}, nil},
		{"three dot range", []string{"topic...side"}, []string{"topic...side"}, nil},
		{"exclusion", []string{"main", "^topic"}, []string{"main", "^topic"}, nil},
		{"branches", []string{"--branches"}, []string{"--branches"}, nil},
		{"tags", []string{"--tags"}, []string{"--tags"}, nil},
		{
			"path limit",
			[]string{"HEAD", "--", "dir/nested.txt"},
			[]string{"HEAD", "--", "dir/nested.txt"},
			nil,
		},
		{
			"path limit on a directory",
			[]string{"HEAD", "--", "dir"},
			[]string{"HEAD", "--", "dir"},
			nil,
		},
		{
			"path limit with first parent",
			[]string{"--first-parent", "HEAD", "--", "dir"},
			[]string{"HEAD", "--", "dir"},
			func(o *Options) { o.FirstParent = true },
		},
		{
			"max count",
			[]string{"--max-count=3", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.MaxCount = 3 },
		},
		{
			"skip",
			[]string{"--skip=2", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Skip = 2 },
		},
		{
			"grep",
			[]string{"--grep=topic", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Grep = regexpMustCompile("topic") },
		},
		{
			"author",
			[]string{"--author=oracle", "HEAD"},
			[]string{"HEAD"},
			func(o *Options) { o.Author = regexpMustCompile("oracle") },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := o.lines(append([]string{"rev-list"}, test.args...)...)
			opts, err := Ranges(test.specs, ctx)
			if err != nil {
				t.Fatalf("Ranges(%v) returned error %v", test.specs, err)
			}
			if test.setup != nil {
				test.setup(&opts)
			}
			var got []string
			for commit, err := range Walk(t.Context(), opts) {
				if err != nil {
					t.Fatalf("Walk returned error %v", err)
				}
				got = append(got, commit.ID.String())
			}
			if !slices.Equal(got, want) {
				t.Errorf("Walk visited\n%s\ngit rev-list visited\n%s",
					strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func TestOracleMergeBaseMatchesGit(t *testing.T) {
	o := buildOracleRepository(t)
	o.checkout("-b", "left", "v1")
	o.commit("left work", map[string]string{"left.txt": "l1"})
	o.checkout("-b", "right", "v1")
	o.commit("right work", map[string]string{"right.txt": "r1"})
	o.merge("left", "merge left into right")
	o.checkout("left")
	o.merge("right", "merge right into left")
	ctx := o.open()
	pairs := [][]string{
		{"main", "topic"},
		{"main", "side"},
		{"topic", "side"},
		{"left", "right"},
		{"main", "v1"},
		{"v1", "main"},
	}
	for _, pair := range pairs {
		t.Run(strings.Join(pair, " "), func(t *testing.T) {
			want := o.lines(append([]string{"merge-base", "--all"}, pair...)...)
			slices.Sort(want)
			ids := make([]hash.ObjectID, 0, len(pair))
			for _, spec := range pair {
				ids = append(ids, o.parse(spec+"^{commit}"))
			}
			bases, err := MergeBase(ctx, ids...)
			if err != nil {
				t.Fatalf("MergeBase returned error %v", err)
			}
			got := make([]string, 0, len(bases))
			for _, base := range bases {
				got = append(got, base.String())
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("MergeBase returned %v, git returned %v", got, want)
			}
		})
	}
	octopus := []string{"main", "topic", "side"}
	want := o.lines(append([]string{"merge-base", "--octopus", "--all"}, octopus...)...)
	slices.Sort(want)
	ids := make([]hash.ObjectID, 0, len(octopus))
	for _, spec := range octopus {
		ids = append(ids, o.parse(spec+"^{commit}"))
	}
	bases, err := MergeBase(ctx, ids...)
	if err != nil {
		t.Fatalf("MergeBase returned error %v", err)
	}
	got := make([]string, 0, len(bases))
	for _, base := range bases {
		got = append(got, base.String())
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("MergeBase of three tips returned %v, git returned %v", got, want)
	}
}

func TestOracleIsAncestorMatchesGit(t *testing.T) {
	o := buildOracleRepository(t)
	ctx := o.open()
	pairs := [][2]string{
		{"v1", "main"},
		{"main", "v1"},
		{"topic", "main"},
		{"main", "topic"},
		{"side", "main"},
		{"topic", "side"},
		{"main", "main"},
	}
	for _, pair := range pairs {
		t.Run(pair[0]+" "+pair[1], func(t *testing.T) {
			want := o.succeeds("merge-base", "--is-ancestor", pair[0], pair[1])
			got, err := IsAncestor(ctx, o.parse(pair[0]+"^{commit}"), o.parse(pair[1]+"^{commit}"))
			if err != nil {
				t.Fatalf("IsAncestor returned error %v", err)
			}
			if got != want {
				t.Errorf("IsAncestor(%s, %s) = %v, git reports %v", pair[0], pair[1], got, want)
			}
		})
	}
}

func TestOracleWalkMatchesRevListOnAShallowClone(t *testing.T) {
	o := buildOracleRepository(t)
	clonePath := filepath.Join(o.root, "shallow-clone")
	o.run(o.root, "clone", "-q", "--depth=1", "file://"+filepath.ToSlash(o.repo), clonePath)

	r, err := gitrepo.Open(clonePath, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatalf("repo.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	shallow, err := r.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	if len(shallow) == 0 {
		t.Fatal("git clone --depth=1 did not leave a shallow file behind")
	}
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir()})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := Context{Objects: db, Refs: store, Shallow: shallow}

	want := o.linesIn(clonePath, "log", "--format=%H", "HEAD")
	opts, err := Ranges([]string{"HEAD"}, ctx)
	if err != nil {
		t.Fatalf("Ranges returned error %v", err)
	}
	var got []string
	for commit, err := range Walk(t.Context(), opts) {
		if err != nil {
			t.Fatalf("Walk returned error %v", err)
		}
		got = append(got, commit.ID.String())
	}
	if !slices.Equal(got, want) {
		t.Errorf("Walk visited\n%s\ngit log visited\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
