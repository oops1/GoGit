package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/config"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
)

func operationLogOfCredentialHelperReport(t *testing.T, a *App, rawURL string) []string {
	t.Helper()
	views := captureOperationViews(t)
	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		a.reportIgnoredCredentialHelpers(reporter, rawURL)
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	return readOnDispatcher(t, a, view.Lines)
}

func openRepositoryWithCredentialConfig(t *testing.T, content string) *App {
	t.Helper()
	isolateGitConfig(t)
	target := filepath.Join(t.TempDir(), "repo")
	initTestRepoWithBranch(t, target, "main")
	appendGitConfig(t, target, content)
	a := newTestApp(t)
	a.setOpened(openTestRepository(t, target))
	return a
}

func TestOperationLogNamesCredentialHelpersThatNeedAProgram(t *testing.T) {
	a := openRepositoryWithCredentialConfig(t, "[credential]\n\thelper = cache --timeout=60\n\thelper = store\n\thelper = !f() { echo password=secret; }; f\n")
	a.cfg.Git.CredentialSource = config.CredentialSourceVaultThenHelper

	lines := operationLogOfCredentialHelperReport(t, a, "https://example.com/org/repo.git")
	want := i18n.Tf("Operation.Log.CredentialHelperIgnored", "cache --timeout=60, !f()")
	if !slices.Contains(lines, want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for _, line := range lines {
		if strings.Contains(line, "secret") {
			t.Fatalf("the shell helper body leaked into the log: %q", line)
		}
	}
}

func TestOperationLogStaysQuietAboutCredentialHelpersWhenTheyAreNotUsed(t *testing.T) {
	cases := []struct {
		name   string
		source string
		config string
		rawURL string
	}{
		{"vault only", config.CredentialSourceVault, "[credential]\n\thelper = cache\n", "https://example.com/repo.git"},
		{"ssh remote", config.CredentialSourceHelper, "[credential]\n\thelper = cache\n", "ssh://git@example.com/repo.git"},
		{"no remote url", config.CredentialSourceHelper, "[credential]\n\thelper = cache\n", ""},
		{"only native helpers", config.CredentialSourceHelper, "[credential]\n\thelper = store\n", "https://example.com/repo.git"},
		{"unreadable credential config", config.CredentialSourceHelper, "[credential]\n\thelper = cache\n\tusehttppath = bogus\n", "https://example.com/repo.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := openRepositoryWithCredentialConfig(t, c.config)
			a.cfg.Git.CredentialSource = c.source
			for _, line := range operationLogOfCredentialHelperReport(t, a, c.rawURL) {
				if strings.Contains(line, "credential.helper") {
					t.Fatalf("unexpected line %q", line)
				}
			}
		})
	}
}

func TestIgnoredHelperLabelHidesShellCommandArguments(t *testing.T) {
	cases := map[string]string{
		"cache":                  "cache",
		"!  helper --token=abc":  "!helper",
		"!":                      "!",
		"/opt/bin/custom --flag": "/opt/bin/custom --flag",
	}
	for name, want := range cases {
		if got := ignoredHelperLabel(name); got != want {
			t.Errorf("ignoredHelperLabel(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRemoteRawURLForCredentialsReadsTheDefaultRemote(t *testing.T) {
	a := openRepositoryWithCredentialConfig(t, "[remote \"origin\"]\n\turl = https://example.com/org/repo.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	if got := a.remoteRawURLForCredentials(a.opened()); got != "https://example.com/org/repo.git" {
		t.Fatalf("url = %q", got)
	}

	bare := openRepositoryWithCredentialConfig(t, "")
	if got := bare.remoteRawURLForCredentials(bare.opened()); got != "" {
		t.Fatalf("url without remotes = %q, want empty", got)
	}

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { openGitRepository = prev })
	if got := a.remoteRawURLForCredentials(a.opened()); got != "" {
		t.Fatalf("url when the repository cannot be opened = %q, want empty", got)
	}
}
