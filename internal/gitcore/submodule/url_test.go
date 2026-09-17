package submodule

import (
	"errors"
	"testing"
)

type relativeURLCase struct {
	up     string
	remote string
	url    string
	want   string
}

var sharedRelativeURLCases = []relativeURLCase{
	{"../", "../foo", "../submodule", "../../submodule"},
	{"../", "../foo/bar", "../submodule", "../../foo/submodule"},
	{"../", "../foo/submodule", "../submodule", "../../foo/submodule"},
	{"../", "./foo", "../submodule", "../submodule"},
	{"../", "./foo/bar", "../submodule", "../foo/submodule"},
	{"../../../", "../foo/bar", "../sub/a/b/c", "../../../../foo/sub/a/b/c"},
	{"../", "/abs/addtest", "../repo", "/abs/repo"},
	{"../", "foo/bar", "../submodule", "../foo/submodule"},
	{"../", "foo", "../submodule", "../submodule"},
	{"", "../foo/bar", "../sub/a/b/c", "../foo/sub/a/b/c"},
	{"", "../foo/bar", "../sub/a/b/c/", "../foo/sub/a/b/c"},
	{"", "../foo/bar/", "../sub/a/b/c", "../foo/sub/a/b/c"},
	{"", "../foo/bar", "../submodule", "../foo/submodule"},
	{"", "../foo", "../submodule", "../submodule"},
	{"", "./foo/bar", "../submodule", "foo/submodule"},
	{"", "./foo", "../submodule", "submodule"},
	{"", "//somewhere else/repo", "../subrepo", "//somewhere else/subrepo"},
	{"", "//somewhere else/repo", "../../subrepo", "//subrepo"},
	{"", "//somewhere else/repo", "../../../subrepo", "/subrepo"},
	{"", "//somewhere else/repo", "../../../../subrepo", "subrepo"},
	{"", "/abs/.", "../.", "/abs/."},
	{"", "/abs", "./.", "/abs/."},
	{"", "/abs", "./å äö", "/abs/å äö"},
	{"", "/abs/home2/../remote", "../bundle1", "/abs/home2/../bundle1"},
	{"", "file:///tmp/repo", "../subrepo", "file:///tmp/subrepo"},
	{"", "foo/bar", "../submodule", "foo/submodule"},
	{"", "foo", "../submodule", "submodule"},
	{"", "helper:://hostname/repo", "../subrepo", "helper:://hostname/subrepo"},
	{"", "helper:://hostname/repo", "../../subrepo", "helper:://subrepo"},
	{"", "helper:://hostname/repo", "../../../subrepo", "helper::/subrepo"},
	{"", "helper:://hostname/repo", "../../../../subrepo", "helper::subrepo"},
	{"", "helper:://hostname/repo", "../../../../../subrepo", "helper:subrepo"},
	{"", "helper:://hostname/repo", "../../../../../../subrepo", ".:subrepo"},
	{"", "ssh://hostname/repo", "../subrepo", "ssh://hostname/subrepo"},
	{"", "ssh://hostname/repo", "../../subrepo", "ssh://subrepo"},
	{"", "ssh://hostname/repo", "../../../subrepo", "ssh:/subrepo"},
	{"", "ssh://hostname/repo", "../../../../subrepo", "ssh:subrepo"},
	{"", "ssh://hostname/repo", "../../../../../subrepo", ".:subrepo"},
	{"", "ssh://hostname:22/repo", "../subrepo", "ssh://hostname:22/subrepo"},
	{"", "user@host:path/to/repo", "../subrepo", "user@host:path/to/subrepo"},
	{"", "user@host:repo", "../subrepo", "user@host:subrepo"},
	{"", "user@host:repo", "../../subrepo", ".:subrepo"},
	{"", "https://example.com/group/super.git/", "../lib.git", "https://example.com/group/lib.git"},
	{"../", "https://example.com/group/super.git", "./lib", "https://example.com/group/super.git/lib"},
	{"", "https://example.com/super", "https://other.example.com/lib", "https://other.example.com/lib"},
	{"", "https://example.com/super", "git@host:lib", "git@host:lib"},
	{"", "https://example.com/super", "/srv/lib", "/srv/lib"},
	{"", "https://example.com/super", "lib", "https://example.com/super/lib"},
}

func TestRelativeURLMatchesGitOnEveryPlatform(t *testing.T) {
	for _, style := range []pathStyle{{windows: false}, {windows: true}} {
		for _, tt := range sharedRelativeURLCases {
			got, err := style.relativeURL(tt.remote, tt.url, tt.up)
			if err != nil || got != tt.want {
				t.Fatalf("windows=%v relativeURL(%q, %q, %q) = %q, %v; want %q", style.windows, tt.remote, tt.url, tt.up, got, err, tt.want)
			}
		}
	}
}

func TestRelativeURLTreatsBackslashesAndDrivesOnlyOnWindows(t *testing.T) {
	windows := pathStyle{windows: true}
	unix := pathStyle{windows: false}
	tests := []struct {
		style  pathStyle
		up     string
		remote string
		url    string
		want   string
	}{
		{windows, "", "C:/work/super", "../lib", "C:/work/lib"},
		{windows, "", `C:\work\super\`, `..\lib`, `C:\work/lib`},
		{windows, "", "C:/work/super", `C:\elsewhere\lib`, `C:\elsewhere\lib`},
		{windows, "", "C:/work/super", `\\server\share\lib`, `\\server\share\lib`},
		{windows, "../", `..\foo\bar`, "../submodule", `../..\foo/submodule`},
		{windows, "", "C:/work/super", "./lib", "C:/work/super/lib"},
		{unix, "", "C:/work/super", "../lib", "C:/work/lib"},
		{unix, "", `/work/super\x`, `..\lib`, `/work/super\x/..\lib`},
		{unix, "", "/work/super", `C:\lib`, `C:\lib`},
		{windows, "", "C:/work/super", "C:lib", "C:lib"},
	}
	for _, tt := range tests {
		got, err := tt.style.relativeURL(tt.remote, tt.url, tt.up)
		if err != nil || got != tt.want {
			t.Fatalf("windows=%v relativeURL(%q, %q, %q) = %q, %v; want %q", tt.style.windows, tt.remote, tt.url, tt.up, got, err, tt.want)
		}
	}
}

func TestRelativeURLFailsWhereGitDies(t *testing.T) {
	style := pathStyle{}
	if _, err := style.relativeURL("", "../lib", ""); !errors.Is(err, ErrEmptyRemoteURL) {
		t.Fatalf("empty remote returned %v", err)
	}
	for _, remote := range []string{"foo", ".", "./foo"} {
		if _, err := style.relativeURL(remote, "../../../lib", ""); !errors.Is(err, ErrCannotStripURL) {
			t.Fatalf("relativeURL(%q) returned %v, want ErrCannotStripURL", remote, err)
		}
	}
}

func TestResolveRelativeURLUsesTheNativeStyle(t *testing.T) {
	got, err := ResolveRelativeURL("https://example.com/a/super", "../lib", "")
	if err != nil || got != "https://example.com/a/lib" {
		t.Fatalf("ResolveRelativeURL = %q, %v", got, err)
	}
}

func TestIsRelativeURLAcceptsBothSeparators(t *testing.T) {
	tests := map[string]bool{
		"./lib":   true,
		"../lib":  true,
		`.\lib`:   true,
		`..\lib`:  true,
		"lib":     false,
		".lib":    false,
		"..":      false,
		"https:x": false,
	}
	for url, want := range tests {
		if got := IsRelativeURL(url); got != want {
			t.Fatalf("IsRelativeURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestUpPathClimbsOutOfTheSubmodulePath(t *testing.T) {
	tests := map[string]string{
		"sub":      "../",
		"a/b/c":    "../../../",
		"a/b/":     "../../",
		"":         "../",
		"libs/sub": "../../",
	}
	for path, want := range tests {
		if got := UpPath(path); got != want {
			t.Fatalf("UpPath(%q) = %q, want %q", path, got, want)
		}
	}
}
