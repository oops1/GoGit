//go:build oracle

package refspec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type oracle struct {
	t   *testing.T
	env []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	home := t.TempDir()
	return &oracle{
		t: t,
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=gc.auto",
			"GIT_CONFIG_VALUE_0=0",
			"GIT_CONFIG_KEY_1=maintenance.auto",
			"GIT_CONFIG_VALUE_1=false",
			"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
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
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		o.t.Fatalf("git %s returned error %v, output: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (o *oracle) tryRun(dir string, args ...string) (string, error) {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (o *oracle) sourceRepo() string {
	o.t.Helper()
	root := o.t.TempDir()
	src := filepath.Join(root, "src")
	o.run(root, "init", "-q", "-b", "main", src)
	writeFile(o.t, filepath.Join(src, "a.txt"), "hello\n")
	o.run(src, "add", "a.txt")
	o.run(src, "commit", "-q", "-m", "first")
	o.run(src, "tag", "v1.0.0")
	o.run(src, "checkout", "-q", "-b", "feature/foo")
	writeFile(o.t, filepath.Join(src, "b.txt"), "foo\n")
	o.run(src, "add", "b.txt")
	o.run(src, "commit", "-q", "-m", "foo")
	prHead := strings.TrimSpace(o.run(src, "rev-parse", "HEAD"))
	o.run(src, "checkout", "-q", "-b", "feature/bar", "main")
	writeFile(o.t, filepath.Join(src, "c.txt"), "bar\n")
	o.run(src, "add", "c.txt")
	o.run(src, "commit", "-q", "-m", "bar")
	o.run(src, "checkout", "-q", "main")
	o.run(src, "update-ref", "refs/pull/7/head", prHead)
	return src
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}

func (o *oracle) refs(dir string) map[string]string {
	o.t.Helper()
	out := o.run(dir, "for-each-ref", "--format=%(refname) %(objectname)")
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		name, oid, ok := strings.Cut(line, " ")
		if !ok {
			o.t.Fatalf("unexpected for-each-ref line %q", line)
		}
		refs[name] = oid
	}
	return refs
}

func expectedMapping(t *testing.T, spec RefSpec, source map[string]string) map[string]string {
	t.Helper()
	expected := map[string]string{}
	for name, oid := range source {
		dst, ok := spec.MatchSrc(name)
		if !ok {
			continue
		}
		expected[dst] = oid
	}
	return expected
}

func TestOracleFetchMappingMatchesMatchSrc(t *testing.T) {
	cases := []string{
		"+refs/heads/*:refs/remotes/origin/*",
		"refs/heads/main:refs/heads/main",
		"+refs/pull/*/head:refs/remotes/origin/pull/*",
		"refs/tags/*:refs/tags/*",
		"+refs/heads/feature/*:refs/remotes/origin/feature/*",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			o := newOracle(t)
			src := o.sourceRepo()
			sourceRefs := o.refs(src)

			spec, err := Parse(text)
			if err != nil {
				t.Fatalf("Parse(%q) returned error %v", text, err)
			}
			want := expectedMapping(t, spec, sourceRefs)

			dest := filepath.Join(t.TempDir(), "dest.git")
			o.run(t.TempDir(), "init", "-q", "--bare", dest)
			o.run(dest, "fetch", "-q", "--no-tags", src, text)
			got := o.refs(dest)

			if len(got) != len(want) {
				t.Fatalf("git fetch produced %v, our MatchSrc predicted %v", got, want)
			}
			for name, oid := range want {
				gotOID, ok := got[name]
				if !ok {
					t.Fatalf("git fetch did not create %q, our MatchSrc predicted it from %q", name, text)
				}
				if gotOID != oid {
					t.Fatalf("ref %q = %s after fetch, our MatchSrc predicted %s", name, gotOID, oid)
				}
			}
		})
	}
}

func TestOracleRefFormatMatchesCheckRefFormat(t *testing.T) {
	o := newOracle(t)
	dir := t.TempDir()
	names := []string{
		"refs/heads/main",
		"refs/heads/feature/x",
		"refs/heads/*",
		"refs/pull/*/head",
		"refs/heads/@",
		"refs/heads/a./b",
		"refs/heads/x{y}",
		"master",
		"HEAD",
		"@",
		"refs/heads/.x",
		"refs/heads/a..b",
		"refs/heads/x.",
		"refs/heads/x.lock",
		"refs/heads/a b",
		"refs/heads/a~1",
		"refs/heads/a^",
		"refs/heads/a?",
		"refs/heads/a[b",
		"refs/heads/a\\b",
		"/refs/heads/main",
		"refs/heads/main/",
		"refs//heads/main",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			_, gitErr := o.tryRun(dir, "check-ref-format", "--refspec-pattern", "--allow-onelevel", name)
			gitValid := gitErr == nil

			spec, ourErr := Parse(name + ":" + name)
			ourValid := ourErr == nil
			if !ourValid {
				if _, err := Parse(name); err == nil {
					ourValid = true
				}
			}
			_ = spec

			if gitValid != ourValid {
				t.Errorf("name %q: git check-ref-format valid=%v, our Parse valid=%v", name, gitValid, ourValid)
			}
		})
	}
}
