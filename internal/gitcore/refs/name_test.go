package refs

import (
	"errors"
	"testing"
)

func TestCheckFormatAcceptsNamesGitAccepts(t *testing.T) {
	valid := []string{
		"refs/heads/main",
		"refs/heads/feature/deep/name",
		"refs/tags/v1.0.0",
		"refs/remotes/origin/HEAD",
		"refs/heads/a.b",
		"refs/heads/a./b",
		"refs/heads/@",
		"refs/heads/na@me",
		"refs/heads/лог",
		"refs/heads/x{y}",
		"refs/heads/lock.lockx",
		"heads/main",
	}
	for _, name := range valid {
		if err := CheckFormat(name, 0); err != nil {
			t.Errorf("CheckFormat(%q) returned error %v", name, err)
		}
	}
}

func TestCheckFormatRejectsNamesGitRejects(t *testing.T) {
	invalid := map[string]string{
		"empty":              "",
		"single component":   "main",
		"single at":          "@",
		"leading slash":      "/refs/heads/main",
		"trailing slash":     "refs/heads/main/",
		"double slash":       "refs/heads//main",
		"dot component":      "refs/heads/.hidden",
		"dot dot":            "refs/heads/a..b",
		"trailing dot":       "refs/heads/main.",
		"lock suffix":        "refs/heads/main.lock",
		"lock component":     "refs/heads/main.lock/x",
		"at brace":           "refs/heads/a@{1}",
		"backslash":          "refs/heads/a\\b",
		"space":              "refs/heads/a b",
		"tilde":              "refs/heads/a~1",
		"caret":              "refs/heads/a^",
		"colon":              "refs/heads/a:b",
		"question":           "refs/heads/a?",
		"asterisk":           "refs/heads/a*",
		"open bracket":       "refs/heads/a[b",
		"control character":  "refs/heads/a\tb",
		"delete character":   "refs/heads/a\x7f",
		"newline":            "refs/heads/a\nb",
		"nul":                "refs/heads/a\x00b",
		"dot only component": "refs/heads/./x",
	}
	for description, name := range invalid {
		err := CheckFormat(name, 0)
		if !errors.Is(err, ErrInvalidName) {
			t.Errorf("CheckFormat(%q) for %s returned %v, want ErrInvalidName", name, description, err)
		}
	}
}

func TestCheckFormatAllowsSingleComponentWithFlag(t *testing.T) {
	if err := CheckFormat("HEAD", AllowOneLevel); err != nil {
		t.Fatalf("CheckFormat returned error %v", err)
	}
	if err := CheckFormat("", AllowOneLevel); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("CheckFormat of empty name returned %v", err)
	}
}

func TestCheckFormatAllowsOneAsteriskForRefspecPattern(t *testing.T) {
	if err := CheckFormat("refs/heads/*", RefspecPattern); err != nil {
		t.Fatalf("CheckFormat returned error %v", err)
	}
	if err := CheckFormat("refs/*/*", RefspecPattern); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("CheckFormat of two asterisks returned %v", err)
	}
	if err := CheckFormat("refs/heads/*x*", RefspecPattern); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("CheckFormat of two asterisks in one component returned %v", err)
	}
}

func TestValidateAllowsWellKnownOneLevelNames(t *testing.T) {
	for _, name := range []Name{HEAD, FetchHead, OrigHead, MergeHead, CherryPickHead, RebaseHead, BisectHead} {
		if err := name.Validate(); err != nil {
			t.Errorf("%s.Validate() returned error %v", name, err)
		}
		if !name.IsPseudo() || !name.IsPerWorktree() {
			t.Errorf("%s is not reported as a per worktree pseudo reference", name)
		}
	}
	if err := Name("OTHER_HEAD").Validate(); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Validate of an unknown one level name returned %v", err)
	}
}

func TestNameClassifiesNamespaces(t *testing.T) {
	cases := []struct {
		name        Name
		branch      bool
		tag         bool
		remote      bool
		perWorktree bool
		short       string
	}{
		{name: BranchName("main"), branch: true, short: "main"},
		{name: TagName("v1.0"), tag: true, short: "v1.0"},
		{name: RemoteBranchName("origin", "main"), remote: true, short: "origin/main"},
		{name: "refs/notes/commits", short: "refs/notes/commits"},
		{name: "refs/bisect/good", perWorktree: true, short: "refs/bisect/good"},
		{name: "refs/worktree/x", perWorktree: true, short: "refs/worktree/x"},
		{name: "refs/rewritten/x", perWorktree: true, short: "refs/rewritten/x"},
		{name: HEAD, perWorktree: true, short: "HEAD"},
	}
	for _, item := range cases {
		if item.name.IsBranch() != item.branch {
			t.Errorf("%s.IsBranch() is %v", item.name, item.name.IsBranch())
		}
		if item.name.IsTag() != item.tag {
			t.Errorf("%s.IsTag() is %v", item.name, item.name.IsTag())
		}
		if item.name.IsRemote() != item.remote {
			t.Errorf("%s.IsRemote() is %v", item.name, item.name.IsRemote())
		}
		if item.name.IsPerWorktree() != item.perWorktree {
			t.Errorf("%s.IsPerWorktree() is %v", item.name, item.name.IsPerWorktree())
		}
		if item.name.Short() != item.short {
			t.Errorf("%s.Short() is %q", item.name, item.name.Short())
		}
		if item.name.String() != string(item.name) {
			t.Errorf("%s.String() is %q", item.name, item.name.String())
		}
	}
}
