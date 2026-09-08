package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/reposettings"
)

func localConfigPath(main string) string {
	return filepath.Join(main, ".git", "config")
}

func readLocalConfig(t *testing.T, main string) *gitconfig.File {
	t.Helper()
	data, err := os.ReadFile(localConfigPath(main))
	if err != nil {
		t.Fatal(err)
	}
	file, err := gitconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func captureRepoSettingsView(t *testing.T) **reposettings.View {
	t.Helper()
	captured := new(*reposettings.View)
	prev := newRepoSettingsView
	newRepoSettingsView = func() (*reposettings.View, error) {
		view, err := prev()
		*captured = view
		return view, err
	}
	t.Cleanup(func() { newRepoSettingsView = prev })
	return captured
}

func TestTheSettingsOfARepositoryReachTheDialog(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	settings := reposettings.Settings{
		Name:         "Main",
		UserName:     "Ann",
		UserEmail:    "ann@example.com",
		PullStrategy: reposettings.PullRebase,
		AutoFetch:    reposettings.AutoFetchOff,
	}
	if err := writeRepoSettings(localConfigPath(main), settings); err != nil {
		t.Fatal(err)
	}
	captured := captureRepoSettingsView(t)

	a.openRepoSettings("r1")

	got := (*captured).Settings()
	if got.Name != "Main" || got.UserName != "Ann" || got.UserEmail != "ann@example.com" {
		t.Fatalf("settings = %+v", got)
	}
	if got.PullStrategy != reposettings.PullRebase || got.AutoFetch != reposettings.AutoFetchOff {
		t.Fatalf("settings = %+v, want what the repository holds", got)
	}
}

func TestARepositoryThatCannotBeOpenedIsReported(t *testing.T) {
	a, _ := appWithGroups(t)
	_, failures := captureWorktreeMessages(a)
	opened := false
	prev := newRepoSettingsView
	newRepoSettingsView = func() (*reposettings.View, error) {
		opened = true
		return nil, nil
	}
	t.Cleanup(func() { newRepoSettingsView = prev })

	a.openRepoSettings("r1")

	if opened || len(*failures) != 1 {
		t.Fatalf("opened = %v, failures = %v, want the failure alone", opened, *failures)
	}
}

func TestTheSettingsOfAGroupAreNotOffered(t *testing.T) {
	a, _ := appWithGroups(t)
	opened := false
	prev := newRepoSettingsView
	newRepoSettingsView = func() (*reposettings.View, error) {
		opened = true
		return nil, nil
	}
	t.Cleanup(func() { newRepoSettingsView = prev })

	a.openRepoSettings("work")
	a.openRepoSettings("missing")

	if opened {
		t.Fatal("a group has no repository settings")
	}
}

func TestADialogThatCannotOpenLeavesTheRepositoryAlone(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	prev := newRepoSettingsView
	newRepoSettingsView = func() (*reposettings.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newRepoSettingsView = prev })

	a.openRepoSettings("r1")
}

func TestSavingWritesTheIdentityAndTheChoicesToTheRepository(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)

	a.saveRepoSettings("r1", filepath.Join(main, ".git"), reposettings.Settings{
		Name:          "Renamed",
		UserName:      "Ann",
		UserEmail:     "ann@example.com",
		DefaultRemote: "backup",
		PullStrategy:  reposettings.PullMerge,
		AutoFetch:     reposettings.AutoFetchOn,
	})

	file := readLocalConfig(t, main)
	for key, want := range map[string]string{
		keyUserName:    "Ann",
		keyUserEmail:   "ann@example.com",
		keyPushDefault: "backup",
		keyPullRebase:  "false",
		keyPullFF:      "true",
		keyAutoFetch:   "true",
	} {
		if got, ok := file.Get(key); !ok || got != want {
			t.Fatalf("%s = %q (%v), want %q", key, got, ok, want)
		}
	}
	node, ok := a.registry.Find("r1")
	if !ok || node.Name != "Renamed" {
		t.Fatalf("node = %+v, want the new name", node)
	}
}

func TestInheritedChoicesLeaveNothingInTheRepository(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	a.saveRepoSettings("r1", filepath.Join(main, ".git"), reposettings.Settings{
		Name:          "Main",
		UserName:      "Ann",
		UserEmail:     "ann@example.com",
		DefaultRemote: "backup",
		PullStrategy:  reposettings.PullRebase,
		AutoFetch:     reposettings.AutoFetchOn,
	})

	a.saveRepoSettings("r1", filepath.Join(main, ".git"), reposettings.Settings{Name: "Main"})

	file := readLocalConfig(t, main)
	for _, key := range []string{keyUserName, keyUserEmail, keyPushDefault, keyPullRebase, keyPullFF, keyAutoFetch} {
		if value, ok := file.Get(key); ok {
			t.Fatalf("%s = %q, want it gone", key, value)
		}
	}
}

func TestEveryPullStrategyIsWrittenTheWayGitReadsIt(t *testing.T) {
	main := t.TempDir()
	path := filepath.Join(main, "config")
	for _, c := range []struct {
		strategy string
		rebase   string
		ff       string
	}{
		{reposettings.PullFF, "", "only"},
		{reposettings.PullMerge, "false", "true"},
		{reposettings.PullRebase, "true", ""},
		{reposettings.PullInherit, "", ""},
	} {
		if err := writeRepoSettings(path, reposettings.Settings{Name: "Main", PullStrategy: c.strategy}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := gitconfig.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		rebase, _ := file.Get(keyPullRebase)
		ff, _ := file.Get(keyPullFF)
		if rebase != c.rebase || ff != c.ff {
			t.Fatalf("%q: pull.rebase = %q, pull.ff = %q, want %q and %q", c.strategy, rebase, ff, c.rebase, c.ff)
		}
		if got := pullStrategyOf(file); got != c.strategy {
			t.Fatalf("read back %q, want %q", got, c.strategy)
		}
	}
}

func TestTheAutoFetchChoiceIsReadBackAsItWasWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	for _, choice := range []string{reposettings.AutoFetchOn, reposettings.AutoFetchOff, reposettings.AutoFetchInherit} {
		if err := writeRepoSettings(path, reposettings.Settings{Name: "Main", AutoFetch: choice}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := gitconfig.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := autoFetchOf(file); got != choice {
			t.Fatalf("auto-fetch = %q, want %q", got, choice)
		}
	}
}

func TestAConfigThatCannotBeReadStopsTheSave(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	_, failures := captureWorktreeMessages(a)
	prev := readConfigFile
	readConfigFile = func(string) ([]byte, error) { return nil, errors.New("unreadable") }
	t.Cleanup(func() { readConfigFile = prev })

	a.saveRepoSettings("r1", filepath.Join(main, ".git"), reposettings.Settings{Name: "Renamed"})

	if len(*failures) != 1 {
		t.Fatalf("failures = %v, want the save failure", *failures)
	}
	if node, _ := a.registry.Find("r1"); node.Name != "Main" {
		t.Fatalf("name = %q, want it unchanged", node.Name)
	}
}

func TestAConfigThatIsBrokenStopsTheSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("[user\nname = Ann\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeRepoSettings(path, reposettings.Settings{Name: "Main"}); err == nil {
		t.Fatal("a broken config must not be overwritten silently")
	}
}

func TestAMissingConfigIsWrittenFromScratch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")

	if err := writeRepoSettings(path, reposettings.Settings{Name: "Main", UserName: "Ann", UserEmail: "ann@example.com"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ann@example.com") {
		t.Fatalf("config = %q, want the identity in it", data)
	}
}

func TestARenameTheRegistryRefusesIsLogged(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)

	a.saveRepoSettings("no-such-node", filepath.Join(main, ".git"), reposettings.Settings{Name: "Renamed"})
}

func TestTheDialogSavesAndTheCancelCloses(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	captured := captureRepoSettingsView(t)
	a.openRepoSettings("r1")
	view := *captured

	view.OnOK(reposettings.Settings{Name: "Renamed", UserName: "Ann", UserEmail: "ann@example.com"})

	if node, _ := a.registry.Find("r1"); node.Name != "Renamed" {
		t.Fatalf("name = %q, want the new one", node.Name)
	}
	if value, ok := readLocalConfig(t, main).Get(keyUserName); !ok || value != "Ann" {
		t.Fatalf("user.name = %q, want the one that was entered", value)
	}

	a.openRepoSettings("r1")
	(*captured).OnCancel()
}

func TestTheDefaultRemoteOfTheRepositoryWinsOverTheGlobalOne(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	a.cfg.Git.DefaultRemote = "origin"
	r, err := gitrepo.Open(main, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if got := a.effectiveDefaultRemote(r); got != "origin" {
		t.Fatalf("remote = %q, want the global one", got)
	}

	if err := writeRepoSettings(localConfigPath(main), reposettings.Settings{Name: "Main", DefaultRemote: "backup"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := gitrepo.Open(main, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })

	if got := a.effectiveDefaultRemote(fresh); got != "backup" {
		t.Fatalf("remote = %q, want the one of the repository", got)
	}
}

func TestTheAutoFetchOfTheRepositoryWinsOverTheGlobalOne(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	a.cfg.Git.AutoFetch = false
	o := a.opened()

	if a.autoFetchEnabled(o) {
		t.Fatal("without a repository setting the global one decides")
	}

	if err := writeRepoSettings(localConfigPath(main), reposettings.Settings{Name: "Main", AutoFetch: reposettings.AutoFetchOn}); err != nil {
		t.Fatal(err)
	}

	if !a.autoFetchEnabled(o) {
		t.Fatal("the repository setting must win")
	}

	a.cfg.Git.AutoFetch = true
	if err := writeRepoSettings(localConfigPath(main), reposettings.Settings{Name: "Main", AutoFetch: reposettings.AutoFetchOff}); err != nil {
		t.Fatal(err)
	}

	if a.autoFetchEnabled(o) {
		t.Fatal("a repository that switched it off must stay off")
	}
}

func TestTheInheritedIdentityComesFromOutsideTheRepository(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\tname = Global Ann\n\temail = global@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRepoSettings(localConfigPath(main), reposettings.Settings{Name: "Main", UserName: "Local Ann", UserEmail: "local@example.com"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := gitconfig.Load(gitconfig.Options{
		GitDir:      filepath.Join(main, ".git"),
		WorktreeDir: main,
		GlobalFile:  global,
		NoSystem:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	inherited := a.inheritedSettings(cfg)

	if inherited.UserName != "Global Ann" || inherited.UserEmail != "global@example.com" {
		t.Fatalf("inherited = %+v, want the values from outside the repository", inherited)
	}
	if got := a.localSettings(mustNode(t, a, "r1"), cfg); got.UserName != "Local Ann" {
		t.Fatalf("local = %+v, want the values of the repository", got)
	}
}

func mustNode(t *testing.T, a *App, id string) *repo.Node {
	t.Helper()
	node, ok := a.registry.Find(id)
	if !ok {
		t.Fatalf("node %q is not in the registry", id)
	}
	return node
}

func TestWithoutALocalFileEverythingIsInherited(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	cfg, err := gitconfig.Load(gitconfig.Options{WorktreeDir: main, NoSystem: true})
	if err != nil {
		t.Fatal(err)
	}

	got := a.localSettings(mustNode(t, a, "r1"), cfg)

	if got.UserName != "" || got.PullStrategy != reposettings.PullInherit || got.AutoFetch != reposettings.AutoFetchInherit {
		t.Fatalf("settings = %+v, want everything inherited", got)
	}
	if inherited := a.inheritedSettings(cfg); inherited.UserName != "" {
		t.Fatalf("inherited = %+v, want nothing", inherited)
	}
	if choice := localAutoFetch(cfg); choice != reposettings.AutoFetchInherit {
		t.Fatalf("auto-fetch = %q, want it inherited", choice)
	}
}

func TestTheSettingsOfTheActiveRepositoryOpenFromTheMenu(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	captured := captureRepoSettingsView(t)

	a.openActiveRepoSettings()

	if *captured == nil {
		t.Fatal("the dialog must open for the active repository")
	}
	a.CloseRepository()
	*captured = nil

	a.openActiveRepoSettings()

	if *captured != nil {
		t.Fatal("without an active repository there is nothing to configure")
	}
}

func TestASettingThatCannotBeWrittenStopsTheSave(t *testing.T) {
	prev := applySetting
	applySetting = func(*gitconfig.File, string, string) error { return errors.New("no room") }
	t.Cleanup(func() { applySetting = prev })

	if err := writeRepoSettings(filepath.Join(t.TempDir(), "config"), reposettings.Settings{Name: "Main"}); err == nil {
		t.Fatal("a setting that cannot be written must stop the save")
	}
}

func TestAConfigThatCannotBeSavedIsReported(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeRepoSettings(filepath.Join(blocker, "config"), reposettings.Settings{Name: "Main"}); err == nil {
		t.Fatal("a config that cannot be written must report it")
	}
}

func TestAKeyThatIsNotAConfigNameIsRefused(t *testing.T) {
	file, err := gitconfig.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := setOrUnset(file, "nosection", "value"); err == nil {
		t.Fatal("a key without a section must be refused")
	}
	if err := setOrUnset(file, "nosection", ""); err == nil {
		t.Fatal("unsetting a key without a section must be refused")
	}
}

func TestTheRemotesOfTheRepositoryReachTheDialog(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	data, err := os.ReadFile(localConfigPath(main))
	if err != nil {
		t.Fatal(err)
	}
	file, err := gitconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Set(`remote.origin.url`, "https://example.com/main.git"); err != nil {
		t.Fatal(err)
	}
	if err := file.Save(localConfigPath(main)); err != nil {
		t.Fatal(err)
	}
	captured := captureRepoSettingsView(t)

	a.openRepoSettings("r1")

	items := (*captured).Settings()
	if items.Name != "Main" {
		t.Fatalf("settings = %+v", items)
	}
	cfg, err := gitconfig.Load(gitconfig.Options{GitDir: filepath.Join(main, ".git"), WorktreeDir: main, NoSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	if names := remoteNames(cfg); len(names) != 1 || names[0] != "origin" {
		t.Fatalf("remotes = %v, want origin", names)
	}
}

func TestAutoFetchFallsBackToTheGlobalChoiceWhenTheRepositoryIsGone(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	a.cfg.Git.AutoFetch = true

	if !a.autoFetchEnabled(&openedRepository{path: filepath.Join(t.TempDir(), "gone")}) {
		t.Fatal("a repository that cannot be opened leaves the global choice in place")
	}
}
