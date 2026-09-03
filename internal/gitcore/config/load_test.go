package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLevelStringNamesEveryLevel(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelSystem, "system"},
		{LevelGlobal, "global"},
		{LevelLocal, "local"},
		{LevelWorktree, "worktree"},
		{LevelCommand, "command"},
		{Level(42), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.level.String(); got != tc.want {
			t.Errorf("Level(%d).String() = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestLoadAppliesLevelPriority(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	system := writeFile(t, filepath.Join(dir, "system"), "[core]\n\tpager = less\n\tautocrlf = true\n")
	global := writeFile(t, filepath.Join(dir, "global"), "[core]\n\tautocrlf = input\n[user]\n\tname = Ann\n")
	gitDir := filepath.Join(dir, "repo", ".git")
	writeFile(t, filepath.Join(gitDir, "config"), "[core]\n\tautocrlf = false\n")

	cfg, err := Load(Options{SystemFile: system, GlobalFile: global, GitDir: gitDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.autocrlf"); v != "false" {
		t.Errorf("core.autocrlf = %q, want false", v)
	}
	if v, _ := cfg.Get("core.pager"); v != "less" {
		t.Errorf("core.pager = %q, want less", v)
	}
	if v, _ := cfg.Get("user.name"); v != "Ann" {
		t.Errorf("user.name = %q, want Ann", v)
	}
	if got := cfg.GetAll("core.autocrlf"); !slices.Equal(got, []string{"true", "input", "false"}) {
		t.Errorf("GetAll = %q", got)
	}
	origin, ok := cfg.Origin("core.autocrlf")
	if !ok || origin.Level != LevelLocal || origin.Line != 2 {
		t.Errorf("Origin = %+v, ok=%v", origin, ok)
	}
	if origin.Path != filepath.Join(gitDir, "config") {
		t.Errorf("Origin.Path = %q", origin.Path)
	}
	if _, ok := cfg.Origin("core.zz"); ok {
		t.Error("Origin found a missing key")
	}
	if _, ok := cfg.Origin("zz"); ok {
		t.Error("Origin accepted an invalid name")
	}
	if got := cfg.Len(); got != 5 {
		t.Errorf("Len = %d, want 5", got)
	}
	if f, ok := cfg.File(LevelLocal); !ok || f.Path() != filepath.Join(gitDir, "config") {
		t.Errorf("File(LevelLocal) = %v, %v", f, ok)
	}
	if _, ok := cfg.File(LevelWorktree); ok {
		t.Error("File(LevelWorktree) exists without a worktree config")
	}
}

func TestLoadWithoutSourcesIsEmpty(t *testing.T) {
	isolateEnv(t)
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Len() != 0 {
		t.Fatalf("entries = %v", dumpConfig(cfg))
	}
	if _, ok := cfg.Get("core.bare"); ok {
		t.Error("Get found a value in an empty configuration")
	}
	if got := cfg.GetAll("core.bare"); got != nil {
		t.Errorf("GetAll = %q", got)
	}
	if cfg.Has("core.bare") {
		t.Error("Has found a value in an empty configuration")
	}
}

func TestLoadReadsSystemFromEnvironment(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "sys"), "[core]\n\tpager = more\n")
	t.Setenv("GIT_CONFIG_SYSTEM", path)
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.pager"); v != "more" {
		t.Fatalf("core.pager = %q", v)
	}
	if origin, _ := cfg.Origin("core.pager"); origin.Level != LevelSystem {
		t.Fatalf("origin level = %v", origin.Level)
	}
}

func TestLoadSkipsSystemWhenAsked(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "sys"), "[core]\n\tpager = more\n")
	t.Setenv("GIT_CONFIG_SYSTEM", path)

	cfg, err := Load(Options{NoSystem: true})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Has("core.pager") {
		t.Error("NoSystem did not skip the system file")
	}

	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	cfg, err = Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Has("core.pager") {
		t.Error("GIT_CONFIG_NOSYSTEM did not skip the system file")
	}
}

func TestLoadFailsOnBadNoSystemValue(t *testing.T) {
	isolateEnv(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "maybe")
	if _, err := Load(Options{}); !errors.Is(err, ErrInvalidBool) {
		t.Fatalf("error = %v, want ErrInvalidBool", err)
	}
}

func TestLoadUsesDefaultSystemPathWhenUnset(t *testing.T) {
	isolateEnv(t)
	os.Unsetenv("GIT_CONFIG_SYSTEM")
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned no configuration")
	}
}

func TestSystemPathForKnowsBothPlatforms(t *testing.T) {
	if got := systemPathFor("linux", ""); got != "/etc/gitconfig" {
		t.Errorf("linux system path = %q", got)
	}
	if got := systemPathFor("windows", ""); got != "" {
		t.Errorf("windows system path without ProgramData = %q", got)
	}
	got := systemPathFor("windows", "C:\\ProgramData")
	if !strings.Contains(got, "Git") || !strings.HasSuffix(got, "config") {
		t.Errorf("windows system path = %q", got)
	}
	if defaultSystemPath() == "" && systemPathFor("linux", "") == "" {
		t.Error("defaultSystemPath is unusable")
	}
}

func TestLoadReadsGlobalFromXDGAndHome(t *testing.T) {
	isolateEnv(t)
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeFile(t, filepath.Join(xdg, "git", "config"), "[user]\n\tname = FromXDG\n\temail = x@example.com\n")
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = FromHome\n")
	os.Unsetenv("GIT_CONFIG_GLOBAL")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("user.name"); v != "FromHome" {
		t.Errorf("user.name = %q, want FromHome", v)
	}
	if v, _ := cfg.Get("user.email"); v != "x@example.com" {
		t.Errorf("user.email = %q", v)
	}
	if f, ok := cfg.File(LevelGlobal); !ok || f.Path() != filepath.Join(home, ".gitconfig") {
		t.Errorf("global file = %v, %v", f, ok)
	}
}

func TestLoadUsesDefaultXDGLocationWhenUnset(t *testing.T) {
	isolateEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("GIT_CONFIG_GLOBAL")
	writeFile(t, filepath.Join(home, ".config", "git", "config"), "[user]\n\tname = FromDefaultXDG\n")
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("user.name"); v != "FromDefaultXDG" {
		t.Fatalf("user.name = %q", v)
	}
}

func TestLoadReportsBrokenXDGGlobalFile(t *testing.T) {
	isolateEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("GIT_CONFIG_GLOBAL")
	writeFile(t, filepath.Join(home, ".config", "git", "config"), "[a\n")
	if _, err := Load(Options{}); !errors.Is(err, ErrBadSection) {
		t.Fatalf("error = %v, want ErrBadSection", err)
	}
}

func TestLoadWorksWithUnsetNoSystemVariable(t *testing.T) {
	isolateEnv(t)
	os.Unsetenv("GIT_CONFIG_NOSYSTEM")
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "sys"), "[core]\n\tpager = more\n")
	t.Setenv("GIT_CONFIG_SYSTEM", path)
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.pager"); v != "more" {
		t.Fatalf("core.pager = %q", v)
	}
}

func TestGlobalPathsAreEmptyWithoutHome(t *testing.T) {
	isolateEnv(t)
	if got := globalPaths(); len(got) != 0 {
		t.Fatalf("globalPaths = %q, want none", got)
	}
}

func TestLoadHonoursGitConfigGlobal(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "g"), "[user]\n\tname = Env\n")
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Home\n")

	t.Setenv("GIT_CONFIG_GLOBAL", path)
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("user.name"); v != "Env" {
		t.Fatalf("user.name = %q, want Env", v)
	}

	t.Setenv("GIT_CONFIG_GLOBAL", "")
	cfg, err = Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Has("user.name") {
		t.Fatal("an empty GIT_CONFIG_GLOBAL did not disable the global file")
	}
}

func TestLoadReadsWorktreeConfigWhenExtensionEnabled(t *testing.T) {
	isolateEnv(t)
	gitDir := filepath.Join(t.TempDir(), ".git")
	writeFile(t, filepath.Join(gitDir, "config"), "[extensions]\n\tworktreeConfig = true\n[core]\n\tbare = false\n")
	writeFile(t, filepath.Join(gitDir, "config.worktree"), "[core]\n\tbare = true\n")

	cfg, err := Load(Options{GitDir: gitDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.bare"); v != "true" {
		t.Fatalf("core.bare = %q, want true", v)
	}
	if origin, _ := cfg.Origin("core.bare"); origin.Level != LevelWorktree {
		t.Fatalf("origin = %+v", origin)
	}
}

func TestLoadIgnoresWorktreeConfigWhenExtensionMissing(t *testing.T) {
	isolateEnv(t)
	gitDir := filepath.Join(t.TempDir(), ".git")
	writeFile(t, filepath.Join(gitDir, "config"), "[core]\n\tbare = false\n")
	writeFile(t, filepath.Join(gitDir, "config.worktree"), "[core]\n\tbare = true\n")
	cfg, err := Load(Options{GitDir: gitDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.bare"); v != "false" {
		t.Fatalf("core.bare = %q, want false", v)
	}
}

func TestLoadReadsWorktreeConfigFromWorktreeDirWhenSet(t *testing.T) {
	isolateEnv(t)
	commonDir := filepath.Join(t.TempDir(), ".git")
	worktreeDir := filepath.Join(commonDir, "worktrees", "feature")
	writeFile(t, filepath.Join(commonDir, "config"), "[extensions]\n\tworktreeConfig = true\n[core]\n\tbare = false\n")
	writeFile(t, filepath.Join(worktreeDir, "config.worktree"), "[core]\n\tbare = true\n")

	cfg, err := Load(Options{GitDir: commonDir, WorktreeDir: worktreeDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.bare"); v != "true" {
		t.Fatalf("core.bare = %q, want true", v)
	}
	origin, ok := cfg.Origin("core.bare")
	if !ok || origin.Level != LevelWorktree {
		t.Fatalf("origin = %+v, ok=%v", origin, ok)
	}
	if origin.Path != filepath.Join(worktreeDir, "config.worktree") {
		t.Fatalf("origin.Path = %q", origin.Path)
	}
}

func TestLoadIgnoresWorktreeDirConfigWhenExtensionMissing(t *testing.T) {
	isolateEnv(t)
	commonDir := filepath.Join(t.TempDir(), ".git")
	worktreeDir := filepath.Join(commonDir, "worktrees", "feature")
	writeFile(t, filepath.Join(commonDir, "config"), "[core]\n\tbare = false\n")
	writeFile(t, filepath.Join(worktreeDir, "config.worktree"), "[core]\n\tbare = true\n")

	cfg, err := Load(Options{GitDir: commonDir, WorktreeDir: worktreeDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("core.bare"); v != "false" {
		t.Fatalf("core.bare = %q, want false", v)
	}
}

func TestLoadFailsOnBadWorktreeExtensionValue(t *testing.T) {
	isolateEnv(t)
	gitDir := filepath.Join(t.TempDir(), ".git")
	writeFile(t, filepath.Join(gitDir, "config"), "[extensions]\n\tworktreeConfig = perhaps\n")
	if _, err := Load(Options{GitDir: gitDir}); !errors.Is(err, ErrInvalidBool) {
		t.Fatalf("error = %v, want ErrInvalidBool", err)
	}
}

func TestLoadReadsCommandLineVariables(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	global := writeFile(t, filepath.Join(dir, "g"), "[user]\n\tname = File\n")
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "user.name")
	t.Setenv("GIT_CONFIG_VALUE_0", "Env")
	t.Setenv("GIT_CONFIG_KEY_1", "Remote.Origin.URL")
	t.Setenv("GIT_CONFIG_VALUE_1", "git://x")

	cfg, err := Load(Options{GlobalFile: global})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("user.name"); v != "Env" {
		t.Errorf("user.name = %q, want Env", v)
	}
	if v, _ := cfg.Get("remote.Origin.url"); v != "git://x" {
		t.Errorf("remote.Origin.url = %q", v)
	}
	origin, _ := cfg.Origin("user.name")
	if origin.Level != LevelCommand || origin.Path != commandLineOrigin {
		t.Errorf("origin = %+v", origin)
	}
}

func TestLoadRejectsBrokenCommandLineVariables(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{"nonNumericCount", map[string]string{"GIT_CONFIG_COUNT": "x"}, ErrInvalidEnvCount},
		{"negativeCount", map[string]string{"GIT_CONFIG_COUNT": "-1"}, ErrInvalidEnvCount},
		{"missingPair", map[string]string{"GIT_CONFIG_COUNT": "1"}, ErrMissingEnvEntry},
		{
			"missingValue",
			map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "a.b"},
			ErrMissingEnvEntry,
		},
		{
			"invalidKey",
			map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "nodot", "GIT_CONFIG_VALUE_0": "v"},
			ErrInvalidName,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			os.Unsetenv("GIT_CONFIG_KEY_0")
			os.Unsetenv("GIT_CONFIG_VALUE_0")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := Load(Options{}); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestLoadZeroCommandLineVariablesIsAllowed(t *testing.T) {
	isolateEnv(t)
	t.Setenv("GIT_CONFIG_COUNT", "0")
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Len() != 0 {
		t.Fatalf("entries = %v", dumpConfig(cfg))
	}
}

func TestLoadReportsUnreadableAndBrokenFiles(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "asDir"), 0o700); err != nil {
		t.Fatalf("Mkdir returned error %v", err)
	}
	if _, err := Load(Options{SystemFile: filepath.Join(dir, "asDir"), NoSystem: false}); err == nil {
		t.Error("Load accepted a directory as a config file")
	}
	broken := writeFile(t, filepath.Join(dir, "broken"), "[a\n")
	_, err := Load(Options{SystemFile: broken})
	if !errors.Is(err, ErrBadSection) {
		t.Fatalf("error = %v, want ErrBadSection", err)
	}
	if !strings.Contains(err.Error(), broken) {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestLoadFollowsIncludes(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "extra.config"), "[user]\n\temail = inc@example.com\n[core]\n\tpager = delta\n")
	main := writeFile(t, filepath.Join(dir, "main"), "[user]\n\tname = Ann\n[include]\n\tpath = extra.config\n[core]\n\tpager = less\n")

	cfg, err := Load(Options{GlobalFile: main})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	want := []string{
		"user.name=Ann",
		"include.path=extra.config",
		"user.email=inc@example.com",
		"core.pager=delta",
		"core.pager=less",
	}
	if got := dumpConfig(cfg); !slices.Equal(got, want) {
		t.Fatalf("entries = %q, want %q", got, want)
	}
	if v, _ := cfg.Get("core.pager"); v != "less" {
		t.Errorf("core.pager = %q, want less", v)
	}
	origin, _ := cfg.Origin("user.email")
	if origin.Level != LevelGlobal || origin.Path != filepath.Join(dir, "extra.config") {
		t.Errorf("origin = %+v", origin)
	}
}

func TestLoadFollowsNestedIncludeFixtures(t *testing.T) {
	isolateEnv(t)
	cfg, err := Load(Options{GlobalFile: filepath.Join("testdata", "include-level1.config")})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("from.level"); v != "1" {
		t.Errorf("from.level = %q, want 1", v)
	}
	if v, _ := cfg.Get("from.deep"); v != "yes" {
		t.Errorf("from.deep = %q", v)
	}
}

func TestLoadIgnoresMissingIncludeAndNonIncludeKeys(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	main := writeFile(t, filepath.Join(dir, "main"),
		"[include]\n\tpath = absent.config\n\tother = x\n\tpath\n"+
			"[include \"sub\"]\n\tpath = absent2.config\n"+
			"[includeIf]\n\tpath = absent3.config\n"+
			"[other]\n\tpath = absent4.config\n")
	cfg, err := Load(Options{GlobalFile: main})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Len() != 6 {
		t.Fatalf("entries = %v", dumpConfig(cfg))
	}
}

func TestLoadDetectsIncludeCycle(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.config"), "[include]\n\tpath = b.config\n")
	writeFile(t, filepath.Join(dir, "b.config"), "[include]\n\tpath = a.config\n")
	_, err := Load(Options{GlobalFile: filepath.Join(dir, "a.config")})
	if !errors.Is(err, ErrIncludeCycle) {
		t.Fatalf("error = %v, want ErrIncludeCycle", err)
	}
}

func TestLoadLimitsIncludeDepth(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	for i := range 14 {
		writeFile(t, filepath.Join(dir, "c"+string(rune('a'+i))),
			"[include]\n\tpath = c"+string(rune('a'+i+1))+"\n")
	}
	_, err := Load(Options{GlobalFile: filepath.Join(dir, "ca")})
	if !errors.Is(err, ErrIncludeDepth) {
		t.Fatalf("error = %v, want ErrIncludeDepth", err)
	}
}

func TestLoadAllowsTenNestedIncludes(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	for i := range 10 {
		writeFile(t, filepath.Join(dir, "c"+string(rune('a'+i))),
			"[include]\n\tpath = c"+string(rune('a'+i+1))+"\n")
	}
	writeFile(t, filepath.Join(dir, "ck"), "[deep]\n\treached = yes\n")
	cfg, err := Load(Options{GlobalFile: filepath.Join(dir, "ca")})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("deep.reached"); v != "yes" {
		t.Fatalf("deep.reached = %q", v)
	}
}

func TestLoadReportsErrorsInsideIncludes(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.config"), "[a\n")
	main := writeFile(t, filepath.Join(dir, "main"), "[include]\n\tpath = bad.config\n")
	if _, err := Load(Options{GlobalFile: main}); !errors.Is(err, ErrBadSection) {
		t.Fatalf("error = %v, want ErrBadSection", err)
	}
}

func TestLoadRejectsIncludeWithUnexpandablePath(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	main := writeFile(t, filepath.Join(dir, "main"), "[include]\n\tpath = ~someone/x\n")
	if _, err := Load(Options{GlobalFile: main}); !errors.Is(err, ErrExpandUser) {
		t.Fatalf("error = %v, want ErrExpandUser", err)
	}
}

func TestLoadResolvesAbsoluteAndHomeIncludePaths(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "fromhome.config"), "[home]\n\tok = yes\n")
	absolute := writeFile(t, filepath.Join(dir, "abs.config"), "[abs]\n\tok = yes\n")
	main := writeFile(t, filepath.Join(dir, "main"),
		"[include]\n\tpath = ~/fromhome.config\n\tpath = "+filepath.ToSlash(absolute)+"\n")
	cfg, err := Load(Options{GlobalFile: main})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if v, _ := cfg.Get("home.ok"); v != "yes" {
		t.Errorf("home.ok = %q", v)
	}
	if v, _ := cfg.Get("abs.ok"); v != "yes" {
		t.Errorf("abs.ok = %q", v)
	}
}

func TestConditionalIncludesMatchGitDirAndBranch(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		gitDir    string
		branch    string
		want      bool
	}{
		{"gitDirGlobMatches", "gitdir:**/work/**", "work/repo/.git", "main", true},
		{"gitDirGlobMisses", "gitdir:**/other/**", "work/repo/.git", "main", false},
		{"gitDirRelativePatternGetsDoubleStar", "gitdir:work/", "work/repo/.git", "main", true},
		{"gitDirIsCaseSensitive", "gitdir:**/WORK/**", "work/repo/.git", "main", false},
		{"gitDirCaseInsensitive", "gitdir/i:**/WORK/**", "work/repo/.git", "main", true},
		{"gitDirRelativeToConfig", "gitdir:./work/", "work/repo/.git", "main", true},
		{"gitDirEmptyPatternNeverMatches", "gitdir:", "work/repo/.git", "main", false},
		{"onBranchMatches", "onbranch:main", "work/repo/.git", "main", true},
		{"onBranchGlob", "onbranch:release/*", "work/repo/.git", "release/1", true},
		{"onBranchTrailingSlash", "onbranch:feature/", "work/repo/.git", "feature/a/b", true},
		{"onBranchMisses", "onbranch:main", "work/repo/.git", "topic", false},
		{"onBranchEmptyPattern", "onbranch:", "work/repo/.git", "main", false},
		{"unknownConditionIsFalse", "hasconfig:remote.*.url:x", "work/repo/.git", "main", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateEnv(t)
			dir := t.TempDir()
			gitDir := filepath.Join(dir, filepath.FromSlash(tc.gitDir))
			if err := os.MkdirAll(gitDir, 0o700); err != nil {
				t.Fatalf("MkdirAll returned error %v", err)
			}
			writeFile(t, filepath.Join(dir, "cond.config"), "[cond]\n\tapplied = yes\n")
			main := writeFile(t, filepath.Join(dir, "main"),
				"[includeIf \""+tc.condition+"\"]\n\tpath = cond.config\n")
			cfg, err := Load(Options{GlobalFile: main, GitDir: gitDir, Branch: tc.branch})
			if err != nil {
				t.Fatalf("Load returned error %v", err)
			}
			if got := cfg.Has("cond.applied"); got != tc.want {
				t.Fatalf("applied = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConditionalIncludeNeedsAGitDir(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cond.config"), "[cond]\n\tapplied = yes\n")
	main := writeFile(t, filepath.Join(dir, "main"), "[includeIf \"gitdir:**\"]\n\tpath = cond.config\n")
	cfg, err := Load(Options{GlobalFile: main})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Has("cond.applied") {
		t.Fatal("a gitdir condition matched without a repository")
	}
}

func TestConditionalIncludeExpandsHomePattern(t *testing.T) {
	isolateEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(home, "src", "repo", ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	writeFile(t, filepath.Join(home, "cond.config"), "[cond]\n\tapplied = yes\n")
	main := writeFile(t, filepath.Join(home, "main"), "[includeIf \"gitdir:~/src/\"]\n\tpath = cond.config\n")
	cfg, err := Load(Options{GlobalFile: main, GitDir: gitDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if !cfg.Has("cond.applied") {
		t.Fatal("the ~/ gitdir condition did not match")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	cfg, err = Load(Options{GlobalFile: main, GitDir: gitDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if cfg.Has("cond.applied") {
		t.Fatal("the ~/ gitdir condition matched without a home directory")
	}
}

func TestConditionalIncludeMatchesAbsolutePattern(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "repo", ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	writeFile(t, filepath.Join(dir, "cond.config"), "[cond]\n\tapplied = yes\n")
	pattern := filepath.ToSlash(realPath(dir)) + "/"
	main := writeFile(t, filepath.Join(dir, "main"),
		"[includeIf \"gitdir:"+pattern+"\"]\n\tpath = cond.config\n")
	cfg, err := Load(Options{GlobalFile: main, GitDir: gitDir})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if !cfg.Has("cond.applied") {
		t.Fatalf("absolute gitdir pattern %q did not match", pattern)
	}
}

func TestIsAbsolutePatternRecognisesRoots(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"/a", true},
		{"C:/a", true},
		{`D:\a`, true},
		{"a/b", false},
		{"C:", false},
		{"1:/a", false},
	}
	for _, tc := range tests {
		if got := isAbsolutePattern(tc.in); got != tc.want {
			t.Errorf("isAbsolutePattern(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCurrentBranchReadsHead(t *testing.T) {
	tests := []struct {
		name string
		head string
		want string
	}{
		{"attachedHead", "ref: refs/heads/topic\n", "topic"},
		{"detachedHead", "0123456789012345678901234567890123456789\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gitDir := filepath.Join(t.TempDir(), ".git")
			writeFile(t, filepath.Join(gitDir, "HEAD"), tc.head)
			l := &loader{opts: Options{GitDir: gitDir}}
			if got := l.currentBranch(); got != tc.want {
				t.Fatalf("currentBranch = %q, want %q", got, tc.want)
			}
			if got := l.currentBranch(); got != tc.want {
				t.Fatalf("cached currentBranch = %q, want %q", got, tc.want)
			}
		})
	}
	empty := &loader{}
	if got := empty.currentBranch(); got != "" {
		t.Errorf("currentBranch without a repository = %q", got)
	}
	missing := &loader{opts: Options{GitDir: filepath.Join(t.TempDir(), "gone")}}
	if got := missing.currentBranch(); got != "" {
		t.Errorf("currentBranch without HEAD = %q", got)
	}
}

func TestSubsectionsAreUniqueAndSorted(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "c"),
		"[remote \"z\"]\n\turl = 1\n[remote \"a\"]\n\turl = 2\n\tfetch = f\n[remote]\n\tx = 1\n")
	cfg, err := Load(Options{GlobalFile: path})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if got := cfg.Subsections("Remote"); !slices.Equal(got, []string{"a", "z"}) {
		t.Fatalf("Subsections = %q", got)
	}
}

func TestLookupReturnsWholeEntry(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "c"), "[a \"S\"]\n\tb\n")
	cfg, err := Load(Options{GlobalFile: path})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	e, ok := cfg.Lookup("a.S.b")
	if !ok || e.HasValue || e.Name() != "a.S.b" {
		t.Fatalf("Lookup = %+v, ok=%v", e, ok)
	}
	if _, ok := cfg.Lookup("a.S.zz"); ok {
		t.Error("Lookup found a missing key")
	}
	if _, ok := cfg.Lookup("zz"); ok {
		t.Error("Lookup accepted an invalid name")
	}
}

func TestConfigDefaultsFallBackWhenKeyIsAbsent(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "c"), "[a]\n\tbad = maybe\n\tnum = zz\n\tpath = ~who/x\n")
	cfg, err := Load(Options{GlobalFile: path})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	if got, err := cfg.boolOr("a.absent", true); err != nil || !got {
		t.Errorf("boolOr = %v, %v", got, err)
	}
	if _, err := cfg.boolOr("a.bad", true); !errors.Is(err, ErrInvalidBool) {
		t.Errorf("boolOr error = %v", err)
	}
	if got := cfg.stringOr("a.absent", "d"); got != "d" {
		t.Errorf("stringOr = %q", got)
	}
	if got, err := cfg.intOr("a.absent", 7); err != nil || got != 7 {
		t.Errorf("intOr = %v, %v", got, err)
	}
	if _, err := cfg.intOr("a.num", 7); !errors.Is(err, ErrInvalidInt) {
		t.Errorf("intOr error = %v", err)
	}
	if got, err := cfg.pathOr("a.absent", "d"); err != nil || got != "d" {
		t.Errorf("pathOr = %v, %v", got, err)
	}
	if _, err := cfg.pathOr("a.path", "d"); !errors.Is(err, ErrExpandUser) {
		t.Errorf("pathOr error = %v", err)
	}
}

func TestLoadReadsGitGeneratedFixtures(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	writeFile(t, filepath.Join(gitDir, "config"), fixture(t, "local.config"))
	writeFile(t, filepath.Join(dir, "global"), fixture(t, "global.config"))
	writeFile(t, filepath.Join(gitDir, "shared.config"), "[shared]\n\tok = yes\n")

	cfg, err := Load(Options{GlobalFile: filepath.Join(dir, "global"), GitDir: gitDir, Branch: "main"})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	checks := map[string]string{
		"user.name":                      "Валерий Чукалин",
		"user.email":                     "oops1.vc@gmail.com",
		"core.autocrlf":                  "input",
		"remote.origin.url":              "https://example.com/a.git",
		"branch.feature/x.remote":        "origin",
		"alias.lg":                       "log --oneline --graph",
		"http.https://example.com.proxy": "http://p:8080",
		"gc.auto":                        "6700",
		"pack.windowMemory":              "100m",
		"shared.ok":                      "yes",
		"url.git@example.com:.insteadOf": "https://example.com/",
		"init.defaultBranch":             "main",
	}
	for key, want := range checks {
		if got, ok := cfg.Get(key); !ok || got != want {
			t.Errorf("Get(%q) = %q, %v, want %q", key, got, ok, want)
		}
	}
	if got := cfg.GetAll("safe.directory"); !slices.Equal(got, []string{"/srv/a", "/srv/b"}) {
		t.Errorf("safe.directory = %q", got)
	}
	if v, err := cfg.GetInt("pack.windowMemory"); err != nil || v != 100<<20 {
		t.Errorf("pack.windowMemory = %v, %v", v, err)
	}
	if origin, _ := cfg.Origin("user.name"); origin.Level != LevelLocal {
		t.Errorf("user.name origin = %+v", origin)
	}
}
