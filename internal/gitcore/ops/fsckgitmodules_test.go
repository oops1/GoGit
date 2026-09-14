package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestSubmoduleNamesFollowGitsRules(t *testing.T) {
	for name, allowed := range map[string]bool{
		"lib": true, "libs/sub": true, "..x": true, "a..": true,
		"": false, "..": false, "../../hooks": false, "a/../b": false, `..\x`: false, `a\..`: false,
	} {
		if got := submoduleNameAllowed(name); got != allowed {
			t.Errorf("submoduleNameAllowed(%q) = %v, want %v", name, got, allowed)
		}
	}
}

func TestSubmoduleURLsFollowGitsRules(t *testing.T) {
	for url, allowed := range map[string]bool{
		"https://example.com/lib.git": true,
		"ssh://host/%0a":              true,
		"git@host:lib.git":            true,
		"../lib.git":                  true,
		"./%zz":                       true,
		"./%00":                       true,
		"./%0":                        true,
		"https://user:pass@host/x":    true,
		"https://host?q@x":            true,
		"https://example.com":         true,
		"-upload-pack=evil":           false,
		"./%0aevil":                   false,
		"../../:host":                 false,
		".././../x/y":                 true,
		"./../../host":                true,
		"./../../:host":               false,
		"git://host/%0A":              false,
		"https:///nohost":             false,
		"https://%0ahost/x":           false,
		"http::https://x%0a/y":        false,
		"ftp://u%0a@h/":               false,
		"https://user:pa%0ass@h/":     false,
		"ftps::x":                     false,
		"http::://host":               false,
	} {
		if got := submoduleURLAllowed(url); got != allowed {
			t.Errorf("submoduleURLAllowed(%q) = %v, want %v", url, got, allowed)
		}
	}
}

func gitmodulesProblems(report FsckReport) []string {
	var out []string
	for _, p := range report.Problems {
		if p.Kind == FsckBadGitmodules {
			out = append(out, p.Check+" "+p.ID.String())
		}
	}
	slices.Sort(out)
	return out
}

func TestFsckReportsTheGitmodulesRulesGitEnforces(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	put := func(kind object.Type, text string) hash.ObjectID {
		id, err := db.Put(kind, []byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	bad := put(object.TypeBlob, "[submodule \"../x\"]\n\turl = -z\n\tpath = -p\n\tupdate = !rm\n\tupdate = rebase\n\tbranch\n[submodule]\n\turl = -y\n[other \"..\"]\n\turl = -y\ngarbage[\n[submodule \"late\"]\n\turl = -w\n")
	good := put(object.TypeBlob, "[submodule \"lib\"]\n\turl = https://example.com/lib\n")
	link := put(object.TypeBlob, "target")
	missing := hash.SumSHA1("blob", []byte("missing"))
	notBlob := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "x", ID: good})
	symlinkTree := putTree(t, r, object.TreeEntry{Mode: object.ModeSymlink, Name: ".GitModules", ID: link})
	reachable := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: ".gitmodules", ID: bad},
		object.TreeEntry{Mode: object.ModeTree, Name: "deep", ID: symlinkTree},
		object.TreeEntry{Mode: object.ModeBlob, Name: "gitmod~1", ID: good},
	)
	r.createBranch("reachable", putCommit(t, r, reachable))
	r.createBranch("missing", putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: ".gitmodules", ID: missing})))
	r.createBranch("notblob", putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: ".gitmodules", ID: notBlob})))
	unreachableBad := put(object.TypeBlob, "[submodule \"a\"]\n\tpath = -q\n")
	unreachableTree := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: ".gitmodules", ID: unreachableBad})
	put(object.TypeTree, "not a tree")

	want := []string{
		FsckGitmodulesBlob + " " + notBlob.String(),
		FsckGitmodulesMissing + " " + missing.String(),
		FsckGitmodulesName + " " + bad.String(),
		FsckGitmodulesName + " " + bad.String(),
		FsckGitmodulesName + " " + bad.String(),
		FsckGitmodulesName + " " + bad.String(),
		FsckGitmodulesName + " " + bad.String(),
		FsckGitmodulesPath + " " + bad.String(),
		FsckGitmodulesPath + " " + unreachableBad.String(),
		FsckGitmodulesSymlink + " " + symlinkTree.String(),
		FsckGitmodulesURL + " " + bad.String(),
		FsckGitmodulesUpdate + " " + bad.String(),
	}
	slices.Sort(want)

	report := mustFsck(t, r)

	if got := gitmodulesProblems(report); !slices.Equal(got, want) {
		t.Fatalf("problems\n got  %v\n want %v", got, want)
	}
	if !slices.Contains(report.Unreachable, unreachableTree) {
		t.Fatalf("unreachable = %v", report.Unreachable)
	}
}
