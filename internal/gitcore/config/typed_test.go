package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func loadText(t *testing.T, text string) *Config {
	t.Helper()
	isolateEnv(t)
	path := writeFile(t, filepath.Join(t.TempDir(), "config"), text)
	cfg, err := Load(Options{GlobalFile: path})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	return cfg
}

func TestRemotesCollectAllUrlsAndRefspecs(t *testing.T) {
	cfg := loadText(t, fixture(t, "local.config"))
	remotes := cfg.Remotes()
	if len(remotes) != 2 {
		t.Fatalf("remotes = %+v", remotes)
	}
	if remotes[0].Name != "origin" || remotes[1].Name != "upstream" {
		t.Fatalf("remote order = %q, %q", remotes[0].Name, remotes[1].Name)
	}
	want := Remote{
		Name:  "origin",
		URLs:  []string{"https://example.com/a.git"},
		Fetch: []string{"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*/head:refs/remotes/origin/pull/*"},
	}
	if !reflect.DeepEqual(remotes[0], want) {
		t.Fatalf("origin = %+v, want %+v", remotes[0], want)
	}
	upstream, ok := cfg.Remote("upstream")
	if !ok {
		t.Fatal("Remote(upstream) not found")
	}
	if !slices.Equal(upstream.PushURLs, []string{"ssh://push.example.com/b.git"}) {
		t.Errorf("pushurl = %q", upstream.PushURLs)
	}
	if _, ok := cfg.Remote("absent"); ok {
		t.Error("Remote found an absent remote")
	}
}

func TestRemotePushRefspecsAreRead(t *testing.T) {
	cfg := loadText(t, "[remote \"o\"]\n\turl = u\n\tpush = refs/heads/main\n\tpush = refs/heads/dev\n")
	r, ok := cfg.Remote("o")
	if !ok {
		t.Fatal("Remote(o) not found")
	}
	if !slices.Equal(r.Push, []string{"refs/heads/main", "refs/heads/dev"}) {
		t.Fatalf("push = %q", r.Push)
	}
}

func TestBranchesReadTrackingSettings(t *testing.T) {
	cfg := loadText(t, fixture(t, "local.config"))
	main, ok := cfg.Branch("main")
	if !ok {
		t.Fatal("Branch(main) not found")
	}
	want := Branch{Name: "main", Remote: "origin", Merge: []string{"refs/heads/main"}, Rebase: "true"}
	if !reflect.DeepEqual(main, want) {
		t.Fatalf("branch = %+v, want %+v", main, want)
	}
	branches := cfg.Branches()
	if len(branches) != 2 || branches[0].Name != "feature/x" || branches[1].Name != "main" {
		t.Fatalf("branches = %+v", branches)
	}
	if _, ok := cfg.Branch("absent"); ok {
		t.Error("Branch found an absent branch")
	}
}

func TestUserReadsIdentity(t *testing.T) {
	cfg := loadText(t, "[user]\n\tname = Ann\n\temail = a@example.com\n\tsigningKey = ABCD\n")
	want := User{Name: "Ann", Email: "a@example.com", SigningKey: "ABCD"}
	if got := cfg.User(); got != want {
		t.Fatalf("User = %+v, want %+v", got, want)
	}
	empty := loadText(t, "")
	if got := empty.User(); got != (User{}) {
		t.Fatalf("User of an empty configuration = %+v", got)
	}
}

func TestCoreReadsDefaultsAndValues(t *testing.T) {
	home := t.TempDir()
	cfg := loadText(t, "")
	want := Core{AutoCRLF: AutoCRLFFalse, EOL: EOLNative, FileMode: true, Symlinks: true}
	got, err := cfg.Core()
	if err != nil {
		t.Fatalf("Core returned error %v", err)
	}
	if got != want {
		t.Fatalf("Core defaults = %+v, want %+v", got, want)
	}

	t.Setenv("HOME", home)
	cfg = loadText(t, "[core]\n\tbare = true\n\trepositoryformatversion = 1\n\tworktree = ../w\n"+
		"\tautocrlf = INPUT\n\teol = CRLF\n\tfilemode = false\n\tsymlinks = no\n\tignorecase = 1\n"+
		"\texcludesFile = ~/.gitignore\n\thooksPath = /srv/hooks\n")
	t.Setenv("HOME", home)
	got, err = cfg.Core()
	if err != nil {
		t.Fatalf("Core returned error %v", err)
	}
	want = Core{
		Bare:                    true,
		RepositoryFormatVersion: 1,
		Worktree:                "../w",
		AutoCRLF:                AutoCRLFInput,
		EOL:                     EOLCRLF,
		FileMode:                false,
		Symlinks:                false,
		IgnoreCase:              true,
		ExcludesFile:            filepath.Join(home, ".gitignore"),
		HooksPath:               "/srv/hooks",
	}
	if got != want {
		t.Fatalf("Core = %+v, want %+v", got, want)
	}
}

func TestCoreAutoCRLFAcceptsBooleans(t *testing.T) {
	for text, want := range map[string]string{
		"[core]\n\tautocrlf = true\n": AutoCRLFTrue,
		"[core]\n\tautocrlf = 0\n":    AutoCRLFFalse,
	} {
		cfg := loadText(t, text)
		got, err := cfg.Core()
		if err != nil {
			t.Fatalf("Core returned error %v", err)
		}
		if got.AutoCRLF != want {
			t.Errorf("autocrlf of %q = %q, want %q", text, got.AutoCRLF, want)
		}
	}
}

func TestCoreRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{"bare", "[core]\n\tbare = perhaps\n", ErrInvalidBool},
		{"repositoryformatversion", "[core]\n\trepositoryformatversion = x\n", ErrInvalidInt},
		{"filemode", "[core]\n\tfilemode = perhaps\n", ErrInvalidBool},
		{"symlinks", "[core]\n\tsymlinks = perhaps\n", ErrInvalidBool},
		{"ignorecase", "[core]\n\tignorecase = perhaps\n", ErrInvalidBool},
		{"worktree", "[core]\n\tworktree = ~who/x\n", ErrExpandUser},
		{"excludesfile", "[core]\n\texcludesFile = ~who/x\n", ErrExpandUser},
		{"hookspath", "[core]\n\thooksPath = ~who/x\n", ErrExpandUser},
		{"autocrlf", "[core]\n\tautocrlf = perhaps\n", ErrInvalidBool},
		{"eol", "[core]\n\teol = mac\n", ErrInvalidValue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadText(t, tc.text)
			if _, err := cfg.Core(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseEOLNormalisesKnownValues(t *testing.T) {
	for _, in := range []string{"lf", "LF", "crlf", "native"} {
		if _, err := parseEOL(in); err != nil {
			t.Errorf("parseEOL(%q) returned error %v", in, err)
		}
	}
}

func TestExtensionsReadKnownKeys(t *testing.T) {
	cfg := loadText(t, "[extensions]\n\tobjectFormat = SHA256\n\tworktreeConfig = true\n\tpreciousObjects\n")
	got, err := cfg.Extensions()
	if err != nil {
		t.Fatalf("Extensions returned error %v", err)
	}
	want := Extensions{ObjectFormat: ObjectFormatSHA256, WorktreeConfig: true, PreciousObjects: true}
	if got != want {
		t.Fatalf("Extensions = %+v, want %+v", got, want)
	}
	defaults, err := loadText(t, "").Extensions()
	if err != nil {
		t.Fatalf("Extensions returned error %v", err)
	}
	if defaults.ObjectFormat != ObjectFormatSHA1 {
		t.Fatalf("default object format = %q", defaults.ObjectFormat)
	}
}

func TestExtensionsIgnoreSubsections(t *testing.T) {
	cfg := loadText(t, "[extensions \"x\"]\n\tsomething = 1\n")
	if _, err := cfg.Extensions(); err != nil {
		t.Fatalf("Extensions returned error %v", err)
	}
}

func TestExtensionsRejectUnknownAndBadValues(t *testing.T) {
	tests := []struct {
		name string
		text string
		want error
	}{
		{"unknownExtension", "[extensions]\n\tnoSuchThing = 1\n", ErrUnknownExtension},
		{"objectFormatWithoutValue", "[extensions]\n\tobjectFormat\n", ErrMissingValue},
		{"unknownObjectFormat", "[extensions]\n\tobjectFormat = sha3\n", ErrInvalidValue},
		{"badWorktreeConfig", "[extensions]\n\tworktreeConfig = perhaps\n", ErrInvalidBool},
		{"badPreciousObjects", "[extensions]\n\tpreciousObjects = perhaps\n", ErrInvalidBool},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadText(t, tc.text)
			if _, err := cfg.Extensions(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
