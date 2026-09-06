package refspec

import (
	"errors"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		name string
		text string
		want RefSpec
	}{
		{"plainBranch", "refs/heads/main:refs/heads/main", RefSpec{Src: "refs/heads/main", Dst: "refs/heads/main"}},
		{"forced", "+refs/heads/main:refs/heads/main", RefSpec{Force: true, Src: "refs/heads/main", Dst: "refs/heads/main"}},
		{"wildcardBoth", "refs/heads/*:refs/remotes/origin/*", RefSpec{Src: "refs/heads/*", Dst: "refs/remotes/origin/*"}},
		{"forcedWildcard", "+refs/heads/*:refs/remotes/origin/*", RefSpec{Force: true, Src: "refs/heads/*", Dst: "refs/remotes/origin/*"}},
		{"deleteBranch", ":refs/heads/gone", RefSpec{Src: "", Dst: "refs/heads/gone"}},
		{"forcedDelete", "+:refs/heads/gone", RefSpec{Force: true, Src: "", Dst: "refs/heads/gone"}},
		{"fetchOnly", "refs/heads/main", RefSpec{Src: "refs/heads/main", Dst: ""}},
		{"fetchOnlyForced", "+refs/heads/main", RefSpec{Force: true, Src: "refs/heads/main", Dst: ""}},
		{"fetchOnlyWildcard", "refs/heads/*", RefSpec{Src: "refs/heads/*", Dst: ""}},
		{"fetchOnlyOneLevel", "master", RefSpec{Src: "master", Dst: ""}},
		{"fetchOnlyHEAD", "HEAD", RefSpec{Src: "HEAD", Dst: ""}},
		{"pushOneLevelDst", "HEAD:refs/heads/master", RefSpec{Src: "HEAD", Dst: "refs/heads/master"}},
		{"pushOneLevelBoth", "master:master", RefSpec{Src: "master", Dst: "master"}},
		{"midSegmentWildcard", "refs/pull/*/head:refs/remotes/origin/pull/*", RefSpec{Src: "refs/pull/*/head", Dst: "refs/remotes/origin/pull/*"}},
		{"forcedMidSegmentWildcard", "+refs/pull/*/head:refs/remotes/origin/pull/*", RefSpec{Force: true, Src: "refs/pull/*/head", Dst: "refs/remotes/origin/pull/*"}},
		{"wildcardOnlyOneLevel", "*:refs/remotes/origin/*", RefSpec{Src: "*", Dst: "refs/remotes/origin/*"}},
		{"noDstButExplicitColon", "master:", RefSpec{Src: "master", Dst: ""}},
		{"plusInName", "++weird:refs/heads/target", RefSpec{Force: true, Src: "+weird", Dst: "refs/heads/target"}},
		{"tag", "refs/tags/v1.0.0:refs/tags/v1.0.0", RefSpec{Src: "refs/tags/v1.0.0", Dst: "refs/tags/v1.0.0"}},
		{"sha1Source", "1111111111111111111111111111111111111111:refs/heads/pinned", RefSpec{Src: "1111111111111111111111111111111111111111", Dst: "refs/heads/pinned"}},
		{"dashInName", "refs/heads/-weird:refs/heads/-weird", RefSpec{Src: "refs/heads/-weird", Dst: "refs/heads/-weird"}},
		{"nestedWildcardMatch", "refs/heads/feature/*:refs/remotes/origin/feature/*", RefSpec{Src: "refs/heads/feature/*", Dst: "refs/remotes/origin/feature/*"}},
		{"atSignComponent", "refs/heads/@:refs/heads/x", RefSpec{Src: "refs/heads/@", Dst: "refs/heads/x"}},
		{"dotAfterSlash", "refs/heads/a./b:refs/heads/a./b", RefSpec{Src: "refs/heads/a./b", Dst: "refs/heads/a./b"}},
		{"reflogLikeBraces", "refs/heads/x{y}:refs/heads/x{y}", RefSpec{Src: "refs/heads/x{y}", Dst: "refs/heads/x{y}"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.text)
			if err != nil {
				t.Fatalf("Parse(%q) returned error %v", c.text, err)
			}
			if got != c.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", c.text, got, c.want)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		"",
		"+",
		":",
		"+:",
		"a:b:c",
		"+a:b:c",
		"::",
		"@:refs/heads/x",
		"refs/heads/**:refs/remotes/origin/**",
		"refs/heads/*x*:refs/remotes/origin/*",
		"refs/heads/*:refs/remotes/origin/*x*",
		"refs/heads/*:refs/remotes/origin/plain",
		"refs/heads/plain:refs/remotes/origin/*",
		":refs/heads/*",
		":refs/heads/main.",
		"refs/heads/main.:refs/heads/main",
		"refs/heads/main:refs/heads/main.",
		"refs/heads/.main:refs/heads/main",
		"refs/heads//main:refs/heads/main",
		"refs/heads/main/:refs/heads/main",
		"/refs/heads/main:refs/heads/main",
		"refs/heads/main..x:refs/heads/main",
		"refs/heads/main.lock:refs/heads/main",
		"refs/heads/ma in:refs/heads/main",
		"refs/heads/a[b:refs/heads/main",
		"refs/heads/a?b:refs/heads/main",
		"refs/heads/a\\b:refs/heads/main",
		"refs/heads/a^b:refs/heads/main",
		"refs/heads/a~b:refs/heads/main",
		"^refs/heads/main:refs/heads/main",
		"refs/heads/main:^refs/heads/main",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			if _, err := Parse(text); err == nil {
				t.Fatalf("Parse(%q) succeeded, want error", text)
			} else if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Parse(%q) returned %v, want it to wrap ErrInvalid", text, err)
			}
		})
	}
}

func TestParseAll(t *testing.T) {
	specs, err := ParseAll([]string{"refs/heads/main:refs/heads/main", "+refs/heads/*:refs/remotes/origin/*"})
	if err != nil {
		t.Fatalf("ParseAll returned error %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("ParseAll returned %d specs, want 2", len(specs))
	}
	if specs[1] != DefaultFetch("origin") {
		t.Fatalf("ParseAll()[1] = %+v, want %+v", specs[1], DefaultFetch("origin"))
	}
}

func TestParseAllStopsAtFirstError(t *testing.T) {
	if _, err := ParseAll([]string{"refs/heads/main:refs/heads/main", "bad::spec"}); err == nil {
		t.Fatalf("ParseAll succeeded, want error")
	}
}

func TestDefaultFetch(t *testing.T) {
	spec := DefaultFetch("origin")
	if spec.String() != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("DefaultFetch(%q).String() = %q", "origin", spec.String())
	}
	if !spec.Force || !spec.IsWildcard() {
		t.Fatalf("DefaultFetch(%q) = %+v, want forced wildcard", "origin", spec)
	}
}

func TestIsWildcard(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"refs/heads/main:refs/heads/main", false},
		{"refs/heads/*:refs/remotes/origin/*", true},
		{":refs/heads/gone", false},
		{"refs/heads/main", false},
		{"refs/heads/*", true},
	}
	for _, c := range cases {
		spec, err := Parse(c.text)
		if err != nil {
			t.Fatalf("Parse(%q) returned error %v", c.text, err)
		}
		if got := spec.IsWildcard(); got != c.want {
			t.Errorf("Parse(%q).IsWildcard() = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestIsDelete(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{":refs/heads/gone", true},
		{"+:refs/heads/gone", true},
		{"refs/heads/main:refs/heads/main", false},
		{"refs/heads/main", false},
	}
	for _, c := range cases {
		spec, err := Parse(c.text)
		if err != nil {
			t.Fatalf("Parse(%q) returned error %v", c.text, err)
		}
		if got := spec.IsDelete(); got != c.want {
			t.Errorf("Parse(%q).IsDelete() = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestMatchSrcExact(t *testing.T) {
	spec, err := Parse("refs/heads/main:refs/remotes/origin/main")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if got, ok := spec.MatchSrc("refs/heads/main"); !ok || got != "refs/remotes/origin/main" {
		t.Fatalf("MatchSrc(refs/heads/main) = %q, %v", got, ok)
	}
	if _, ok := spec.MatchSrc("refs/heads/other"); ok {
		t.Fatalf("MatchSrc(refs/heads/other) matched, want no match")
	}
}

func TestMatchSrcWildcard(t *testing.T) {
	spec := DefaultFetch("origin")
	cases := []struct {
		name    string
		want    string
		matches bool
	}{
		{"refs/heads/main", "refs/remotes/origin/main", true},
		{"refs/heads/feature/x", "refs/remotes/origin/feature/x", true},
		{"refs/tags/v1", "", false},
		{"refs/heads", "", false},
	}
	for _, c := range cases {
		got, ok := spec.MatchSrc(c.name)
		if ok != c.matches || (ok && got != c.want) {
			t.Errorf("MatchSrc(%q) = %q, %v, want %q, %v", c.name, got, ok, c.want, c.matches)
		}
	}
}

func TestMatchSrcMidSegmentWildcard(t *testing.T) {
	spec, err := Parse("+refs/pull/*/head:refs/remotes/origin/pull/*")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	got, ok := spec.MatchSrc("refs/pull/42/head")
	if !ok || got != "refs/remotes/origin/pull/42" {
		t.Fatalf("MatchSrc(refs/pull/42/head) = %q, %v", got, ok)
	}
	if _, ok := spec.MatchSrc("refs/pull/42/merge"); ok {
		t.Fatalf("MatchSrc(refs/pull/42/merge) matched, want no match")
	}
	got, ok = spec.MatchDst("refs/remotes/origin/pull/42")
	if !ok || got != "refs/pull/42/head" {
		t.Fatalf("MatchDst(refs/remotes/origin/pull/42) = %q, %v", got, ok)
	}
}

func TestMatchDstWildcard(t *testing.T) {
	spec := DefaultFetch("origin")
	got, ok := spec.MatchDst("refs/remotes/origin/main")
	if !ok || got != "refs/heads/main" {
		t.Fatalf("MatchDst(refs/remotes/origin/main) = %q, %v", got, ok)
	}
	if _, ok := spec.MatchDst("refs/remotes/upstream/main"); ok {
		t.Fatalf("MatchDst(refs/remotes/upstream/main) matched, want no match")
	}
}

func TestMatchWithoutDstNeverMatches(t *testing.T) {
	spec, err := Parse("refs/heads/main")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if _, ok := spec.MatchSrc("refs/heads/main"); ok {
		t.Fatalf("MatchSrc matched a fetch-only spec, want no match")
	}
	if _, ok := spec.MatchDst("refs/heads/main"); ok {
		t.Fatalf("MatchDst matched a fetch-only spec, want no match")
	}
}

func TestMatchOnDeleteSpecNeverMatches(t *testing.T) {
	spec, err := Parse(":refs/heads/gone")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	if _, ok := spec.MatchSrc("refs/heads/gone"); ok {
		t.Fatalf("MatchSrc matched a delete spec, want no match")
	}
	if _, ok := spec.MatchDst("refs/heads/gone"); ok {
		t.Fatalf("MatchDst matched a delete spec, want no match")
	}
}

func TestStringCanonicalForm(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"refs/heads/main:refs/heads/main", "refs/heads/main:refs/heads/main"},
		{"+refs/heads/*:refs/remotes/origin/*", "+refs/heads/*:refs/remotes/origin/*"},
		{":refs/heads/gone", ":refs/heads/gone"},
		{"refs/heads/main", "refs/heads/main"},
		{"master:", "master"},
	}
	for _, c := range cases {
		spec, err := Parse(c.text)
		if err != nil {
			t.Fatalf("Parse(%q) returned error %v", c.text, err)
		}
		if got := spec.String(); got != c.want {
			t.Errorf("Parse(%q).String() = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestParseStringRoundTrip(t *testing.T) {
	texts := []string{
		"refs/heads/main:refs/heads/main",
		"+refs/heads/main:refs/heads/main",
		"refs/heads/*:refs/remotes/origin/*",
		"+refs/heads/*:refs/remotes/origin/*",
		":refs/heads/gone",
		"+:refs/heads/gone",
		"refs/heads/main",
		"+refs/heads/main",
		"master",
		"HEAD",
		"HEAD:refs/heads/master",
		"refs/pull/*/head:refs/remotes/origin/pull/*",
	}
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			first, err := Parse(text)
			if err != nil {
				t.Fatalf("Parse(%q) returned error %v", text, err)
			}
			second, err := Parse(first.String())
			if err != nil {
				t.Fatalf("Parse(%q) round trip returned error %v", first.String(), err)
			}
			if first != second {
				t.Fatalf("round trip of %q produced %+v then %+v", text, first, second)
			}
			third, err := Parse(second.String())
			if err != nil || third != second {
				t.Fatalf("round trip of %q is not stable: %+v vs %+v (err %v)", text, second, third, err)
			}
		})
	}
}
