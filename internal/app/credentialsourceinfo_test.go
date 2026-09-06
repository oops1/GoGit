package app

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/vault"
)

func isolateGitConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func appendGitConfig(t *testing.T, repoPath, content string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(repoPath, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func TestVaultKeyProtectionLabelWithoutAVaultReturnsNotCreatedYet(t *testing.T) {
	a := newTestApp(t)
	want := i18n.T("Dialog.Settings.Secrets.Slot.None")
	if got := a.vaultKeyProtectionLabel(); got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestVaultKeyProtectionLabelReflectsThePasswordSlot(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	want := i18n.T("Dialog.Settings.Secrets.Slot.Password")
	if got := a.vaultKeyProtectionLabel(); got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestVaultKeyProtectionLabelDedupesRepeatedSlotKinds(t *testing.T) {
	a := newTestApp(t)
	v := createTestVault(t, a.paths.VaultFile())
	a.vaultInst = v
	if err := v.AddSlot(t.Context(), vault.NewPasswordUnlocker([]byte("second password"), vault.TestSlotParams())); err != nil {
		t.Fatal(err)
	}
	want := i18n.T("Dialog.Settings.Secrets.Slot.Password")
	if got := a.vaultKeyProtectionLabel(); got != want {
		t.Fatalf("label = %q, want %q (a repeated kind must not be listed twice)", got, want)
	}
}

func TestOpenRepositoryHelperEntriesWhenTheRepositoryCannotBeReopenedReturnsNil(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { openGitRepository = prev })

	if got := a.openRepositoryHelperEntries(); got != nil {
		t.Fatalf("helpers = %+v, want nil", got)
	}
}

func TestOpenRepositoryHelperEntriesWithoutAnOpenRepositoryReturnsNil(t *testing.T) {
	a := newTestApp(t)
	if got := a.openRepositoryHelperEntries(); got != nil {
		t.Fatalf("helpers = %+v, want nil", got)
	}
}

func TestOpenRepositoryHelperEntriesListsConfiguredHelpersAndMarksUnsupportedOnes(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential]\n\thelper = store\n\thelper = !some-shell-script\n"+
		"[remote \"origin\"]\n\turl = https://example.com/org/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	got := a.openRepositoryHelperEntries()
	if len(got) != 2 {
		t.Fatalf("helpers = %+v, want 2 entries", got)
	}
	if got[0].Name != "store" || !got[0].Supported {
		t.Fatalf("helpers[0] = %+v, want a supported store helper", got[0])
	}
	if got[1].Name != "!some-shell-script" || got[1].Supported {
		t.Fatalf("helpers[1] = %+v, want an unsupported shell helper", got[1])
	}
}

func TestOpenRepositoryHelperEntriesFallBackToAPlaceholderURLWithoutRemotes(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential]\n\thelper = store\n")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	got := a.openRepositoryHelperEntries()
	if len(got) != 1 || got[0].Name != "store" || !got[0].Supported {
		t.Fatalf("helpers = %+v, want the global helper even without a configured remote", got)
	}
}

func TestOpenRepositoryHelperEntriesFallsBackToAnyRemoteWhenTheDefaultIsMissing(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential]\n\thelper = store\n"+
		"[remote \"upstream\"]\n\turl = https://example.com/org/repo.git\n\tfetch = +refs/heads/*:refs/remotes/upstream/*\n")

	a := newTestApp(t)
	a.cfg.Git.DefaultRemote = "origin"
	a.setOpened(openTestRepository(t, target))

	got := a.openRepositoryHelperEntries()
	if len(got) != 1 || got[0].Name != "store" {
		t.Fatalf("helpers = %+v, want the store helper found via the non-default remote", got)
	}
}

func TestOpenRepositoryHelperEntriesReturnsNilAndLogsWhenFromConfigErrors(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, "[credential]\n\tusehttppath = bogus\n")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))
	var buf bytes.Buffer
	a.log = slog.New(slog.NewTextHandler(&buf, nil))

	if got := a.openRepositoryHelperEntries(); got != nil {
		t.Fatalf("helpers = %+v, want nil", got)
	}
	if buf.Len() == 0 {
		t.Fatal("an error resolving helpers must be logged")
	}
}

func TestOpenRepositoryHelperEntriesWithNoHelpersReturnsAnEmptySlice(t *testing.T) {
	isolateGitConfig(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepoWithBranch(t, target, "main")

	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))

	if got := a.openRepositoryHelperEntries(); len(got) != 0 {
		t.Fatalf("helpers = %+v, want none", got)
	}
}

func TestRefreshCredentialSourceInfoDoesNotPanicWithoutAnOpenRepositoryOrVault(t *testing.T) {
	a := newTestApp(t)
	view, err := settings.NewView(a.Engine(), a.languages, settings.Model{})
	if err != nil {
		t.Fatal(err)
	}
	a.refreshCredentialSourceInfo(view)
}

func TestSlotKindKeyMapsEveryKnownSlotKind(t *testing.T) {
	cases := map[vault.SlotKind]string{
		vault.SlotDPAPI:         "Dialog.Settings.Secrets.Slot.DPAPI",
		vault.SlotSecretService: "Dialog.Settings.Secrets.Slot.SecretService",
		vault.SlotFile:          "Dialog.Settings.Secrets.Slot.File",
		vault.SlotPassword:      "Dialog.Settings.Secrets.Slot.Password",
		vault.SlotKind("bogus"): "Dialog.Settings.Secrets.Slot.Password",
	}
	for kind, want := range cases {
		if got := slotKindKey(kind); got != want {
			t.Fatalf("slotKindKey(%q) = %q, want %q", kind, got, want)
		}
	}
}
