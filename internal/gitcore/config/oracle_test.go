//go:build oracle

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type oracle struct {
	t    *testing.T
	dir  string
	home string
	env  []string
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
	return &oracle{
		t:    t,
		dir:  root,
		home: home,
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
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
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s returned error %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func (o *oracle) records(dir string, args ...string) []string {
	o.t.Helper()
	out := o.run(dir, append(args, "--null")...)
	var records []string
	for _, record := range strings.Split(out, "\x00") {
		if record != "" {
			records = append(records, strings.Replace(record, "\n", "=", 1))
		}
	}
	return records
}

func ourRecords(cfg *Config) []string {
	var out []string
	for e := range cfg.All() {
		if e.HasValue {
			out = append(out, e.Name()+"="+e.Value)
		} else {
			out = append(out, e.Name())
		}
	}
	return out
}

func (o *oracle) repo() string {
	o.t.Helper()
	o.run(o.dir, "init", "-q", "repo")
	repo := filepath.Join(o.dir, "repo")
	for _, args := range [][]string{
		{"config", "user.name", "Оракул Тест"},
		{"config", "user.email", "oracle@example.com"},
		{"config", "core.autocrlf", "input"},
		{"config", "core.bigFileThreshold", "512m"},
		{"remote", "add", "origin", "https://example.com/a.git"},
		{"remote", "add", "up-stream", "git@example.com:b.git"},
		{"config", "--add", "remote.origin.fetch", "+refs/pull/*/head:refs/remotes/origin/pull/*"},
		{"config", "--add", "remote.origin.pushurl", "ssh://push.example.com/a.git"},
		{"config", "branch.main.remote", "origin"},
		{"config", "branch.main.merge", "refs/heads/main"},
		{"config", "branch.main.rebase", "true"},
		{"config", "alias.lg", "log --oneline --graph"},
		{"config", "--add", "safe.directory", "/srv/a"},
		{"config", "--add", "safe.directory", "/srv/b"},
		{"config", "quoted.spaced", "  keep  "},
		{"config", "quoted.hash", "a#b;c"},
		{"config", "quoted.escapes", "a\tb\nc\\d\"e"},
		{"config", "empty.value", ""},
		{"config", "include.path", "extra.config"},
	} {
		o.run(repo, args...)
	}
	writeFile(o.t, filepath.Join(repo, ".git", "extra.config"),
		"[included]\n\tfrom = extra\n[core]\n\tpager = delta\n")
	return repo
}

func TestOracleListMatchesLoad(t *testing.T) {
	o := newOracle(t)
	repo := o.repo()
	want := o.records(repo, "config", "--list")

	isolateEnv(t)
	cfg, err := Load(Options{GitDir: filepath.Join(repo, ".git"), NoSystem: true})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if got := ourRecords(cfg); !slices.Equal(got, want) {
		t.Fatalf("git sees\n%q\nwe see\n%q", want, got)
	}
}

func TestOracleShowOriginMatchesOurOrigins(t *testing.T) {
	o := newOracle(t)
	repo := o.repo()
	out := o.run(repo, "config", "--list", "--show-origin", "--null")

	records := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var wantFiles []string
	for i := 0; i < len(records); i += 2 {
		wantFiles = append(wantFiles, filepath.Base(strings.TrimPrefix(records[i], "file:")))
	}

	isolateEnv(t)
	cfg, err := Load(Options{GitDir: filepath.Join(repo, ".git"), NoSystem: true})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	entries := slices.Collect(cfg.All())
	if len(entries) != len(wantFiles) {
		t.Fatalf("git reported %d origins, we have %d entries", len(wantFiles), len(entries))
	}
	for i, want := range wantFiles {
		if got := filepath.Base(entries[i].Origin.Path); got != want {
			t.Errorf("origin of %s = %q, want %q", entries[i].Name(), got, want)
		}
	}
}

func TestOracleReadsWhatWeWrite(t *testing.T) {
	o := newOracle(t)
	repo := o.repo()
	path := filepath.Join(repo, ".git", "config")

	f, err := Parse([]byte(fixture(t, "tricky.config")))
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	writes := []struct{ key, value string }{
		{"user.name", "Записанное Имя"},
		{"core.bare", "false"},
		{"quoted.spaced", "  spaces kept  "},
		{"quoted.marks", "a#b;c"},
		{"quoted.escapes", "tab\there and \\slash and \"quote"},
		{"remote.new.url", "https://example.com/n.git"},
		{"branch.feature/x.remote", "origin"},
		{"section.sub.with.dots.key", "v"},
	}
	for _, w := range writes {
		if err := f.Set(w.key, w.value); err != nil {
			t.Fatalf("Set(%q) returned error %v", w.key, err)
		}
	}
	if err := f.Unset("quoted.novalue"); err != nil {
		t.Fatalf("Unset returned error %v", err)
	}
	if err := f.RemoveSection("tail"); err != nil {
		t.Fatalf("RemoveSection returned error %v", err)
	}
	if err := f.Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}

	for _, w := range writes {
		got := strings.TrimSuffix(o.run(repo, "config", "--get", w.key), "\n")
		if got != w.value {
			t.Errorf("git config --get %s = %q, want %q", w.key, got, w.value)
		}
	}
	records := o.records(repo, "config", "--list")
	if slices.Contains(records, "quoted.novalue") {
		t.Errorf("the unset key is still visible: %q", records)
	}
	for _, record := range records {
		if strings.HasPrefix(record, "tail.") {
			t.Errorf("the removed section is still visible: %q", record)
		}
	}
}

func TestOracleAgreesAfterOurEdits(t *testing.T) {
	o := newOracle(t)
	repo := o.repo()
	path := filepath.Join(repo, ".git", "config")

	f, err := Parse([]byte(fixture(t, "local.config")))
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	for _, step := range []struct {
		name string
		call func() error
	}{
		{"add", func() error { return f.Add("safe.directory", "/srv/c") }},
		{"unsetAll", func() error { return f.UnsetAll("remote.origin.fetch") }},
		{"rename", func() error { return f.RenameSection("branch.main", "branch.trunk") }},
		{"removeInclude", func() error { return f.RemoveSection("include") }},
		{"removeGitdir", func() error { return f.RemoveSection("includeIf.gitdir:/srv/work/**") }},
		{"removeBranchCond", func() error { return f.RemoveSection("includeIf.onbranch:release/*") }},
	} {
		if err := step.call(); err != nil {
			t.Fatalf("%s returned error %v", step.name, err)
		}
	}
	if err := f.Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}

	want := o.records(repo, "config", "--list", "--local")
	isolateEnv(t)
	cfg, err := Load(Options{GitDir: filepath.Join(repo, ".git"), NoSystem: true})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if got := ourRecords(cfg); !slices.Equal(got, want) {
		t.Fatalf("git sees\n%q\nwe see\n%q", want, got)
	}
}

func TestOracleAgreesOnTrickyFixture(t *testing.T) {
	o := newOracle(t)
	path := writeFile(t, filepath.Join(o.dir, "tricky.config"), fixture(t, "tricky.config"))
	want := o.records(o.dir, "config", "--list", "--file", path)

	f, err := Parse([]byte(fixture(t, "tricky.config")))
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if got := dump(f); !slices.Equal(got, want) {
		t.Fatalf("git sees\n%q\nwe see\n%q", want, got)
	}
}

func TestOracleAgreesOnGeneratedFixtures(t *testing.T) {
	for _, fx := range []string{"local.config", "global.config", "crlf.config"} {
		t.Run(fx, func(t *testing.T) {
			o := newOracle(t)
			path := writeFile(t, filepath.Join(o.dir, fx), fixture(t, fx))
			want := o.records(o.dir, "config", "--list", "--file", path)
			f, err := Parse([]byte(fixture(t, fx)))
			if err != nil {
				t.Fatalf("Parse returned error %v", err)
			}
			if got := dump(f); !slices.Equal(got, want) {
				t.Fatalf("git sees\n%q\nwe see\n%q", want, got)
			}
		})
	}
}

func TestOracleAgreesOnConditionalIncludes(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "work")
	repo := filepath.Join(o.dir, "work")
	global := writeFile(t, filepath.Join(o.dir, "gitconfig"),
		"[includeIf \"gitdir:**/work/\"]\n\tpath = matched.config\n"+
			"[includeIf \"gitdir:**/other/\"]\n\tpath = missed.config\n"+
			"[includeIf \"gitdir/i:**/WORK/\"]\n\tpath = icase.config\n"+
			"[includeIf \"onbranch:ma*\"]\n\tpath = branch.config\n")
	writeFile(t, filepath.Join(o.dir, "matched.config"), "[matched]\n\tok = yes\n")
	writeFile(t, filepath.Join(o.dir, "missed.config"), "[missed]\n\tok = yes\n")
	writeFile(t, filepath.Join(o.dir, "icase.config"), "[icase]\n\tok = yes\n")
	writeFile(t, filepath.Join(o.dir, "branch.config"), "[branchcond]\n\tok = yes\n")
	o.env = append(o.env, "GIT_CONFIG_GLOBAL="+global)

	gitSaw := o.records(repo, "config", "--list")
	head := strings.TrimSpace(o.run(repo, "symbolic-ref", "--short", "HEAD"))

	isolateEnv(t)
	cfg, err := Load(Options{
		GlobalFile: global,
		GitDir:     filepath.Join(repo, ".git"),
		NoSystem:   true,
		Branch:     head,
	})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	for _, key := range []string{"matched.ok", "missed.ok", "icase.ok", "branchcond.ok"} {
		want := slices.Contains(gitSaw, key+"=yes")
		if got := cfg.Has(key); got != want {
			t.Errorf("%s: git=%v, we=%v (git saw %q)", key, want, got, gitSaw)
		}
	}
}
