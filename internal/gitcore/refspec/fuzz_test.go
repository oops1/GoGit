package refspec

import "testing"

func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"+",
		":",
		"refs/heads/main:refs/heads/main",
		"+refs/heads/main:refs/heads/main",
		"refs/heads/*:refs/remotes/origin/*",
		":refs/heads/gone",
		"refs/heads/main",
		"master",
		"HEAD:refs/heads/master",
		"refs/pull/*/head:refs/remotes/origin/pull/*",
		"a:b:c",
		"refs/heads/**:refs/remotes/origin/**",
		"refs/heads/*:refs/remotes/origin/plain",
		"^refs/heads/main:refs/heads/main",
		"refs/heads/@:refs/heads/x",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		spec, err := Parse(text)
		if err != nil {
			return
		}
		again, err := Parse(spec.String())
		if err != nil {
			t.Fatalf("Parse(%q) succeeded but its String() %q failed to parse: %v", text, spec.String(), err)
		}
		if again != spec {
			t.Fatalf("Parse(%q) round trip mismatch: %+v then %+v", text, spec, again)
		}
		if spec.IsDelete() && spec.IsWildcard() {
			t.Fatalf("Parse(%q) accepted a wildcard delete spec %+v", text, spec)
		}
	})
}
