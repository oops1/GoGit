package refs

import (
	"strings"
	"testing"
)

var fuzzNameSeeds = map[string]bool{
	"refs/heads/main":     true,
	"refs/tags/v1.0.0":    true,
	"refs/remotes/o/HEAD": true,
	"refs/heads/a./b":     true,
	"refs/heads/@":        true,
	"refs/heads/x{y}":     true,
	"heads/main":          true,
	"refs/heads/лог":      true,
	"":                    false,
	"@":                   false,
	"main":                false,
	"/refs/heads/main":    false,
	"refs/heads/main/":    false,
	"refs//heads/main":    false,
	"refs/heads/.x":       false,
	"refs/heads/a..b":     false,
	"refs/heads/x.":       false,
	"refs/heads/x.lock":   false,
	"refs/heads/a@{0}":    false,
	"refs/heads/a\\b":     false,
	"refs/heads/a b":      false,
	"refs/heads/a~1":      false,
	"refs/heads/a^":       false,
	"refs/heads/a:b":      false,
	"refs/heads/a?":       false,
	"refs/heads/a*":       false,
	"refs/heads/a[b":      false,
	"refs/heads/a\tb":     false,
	"refs/heads/a\x7f":    false,
}

func acceptableRefName(name string) bool {
	if name == "" || name == "@" || strings.HasSuffix(name, ".") {
		return false
	}
	components := strings.Split(name, "/")
	if len(components) < 2 {
		return false
	}
	for _, component := range components {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, lockSuffix) {
			return false
		}
		if strings.Contains(component, "..") || strings.Contains(component, "@{") {
			return false
		}
		if strings.ContainsAny(component, forbiddenRefText+"*\x7f") {
			return false
		}
		for index := range len(component) {
			if component[index] < 0x20 {
				return false
			}
		}
	}
	return true
}

func FuzzCheckRefFormat(f *testing.F) {
	for seed := range fuzzNameSeeds {
		f.Add(seed)
	}
	for name, want := range fuzzNameSeeds {
		if got := CheckFormat(name, 0) == nil; got != want {
			f.Fatalf("CheckFormat(%q) accepted %v, want %v", name, got, want)
		}
	}
	f.Fuzz(func(t *testing.T, name string) {
		accepted := CheckFormat(name, 0) == nil
		if accepted != acceptableRefName(name) {
			t.Fatalf("CheckFormat(%q) accepted %v, want %v", name, accepted, !accepted)
		}
		if !accepted {
			return
		}
		if err := Name(name).Validate(); err != nil {
			t.Fatalf("Validate(%q) returned error %v", name, err)
		}
		if err := CheckFormat(name, AllowOneLevel|RefspecPattern); err != nil {
			t.Fatalf("CheckFormat(%q) with flags returned error %v", name, err)
		}
	})
}

func FuzzParsePackedRefs(f *testing.F) {
	seeds := []string{
		"",
		packedHeaderPlain,
		packedHeaderFull,
		"# pack-refs with: peeled\n",
		"1111111111111111111111111111111111111111 refs/heads/main\n",
		packedHeaderFull +
			"1111111111111111111111111111111111111111 refs/tags/v1\n" +
			"^2222222222222222222222222222222222222222\n",
		"2222222222222222222222222222222222222222\trefs/heads/z\n" +
			"1111111111111111111111111111111111111111 refs/heads/a\n",
		"^1111111111111111111111111111111111111111\n",
		"garbage",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		snapshot, err := parsePackedRefs(data)
		if err != nil {
			return
		}
		for index := 1; index < len(snapshot.refs); index++ {
			if snapshot.refs[index-1].Name >= snapshot.refs[index].Name {
				t.Fatalf("references %s and %s are out of order",
					snapshot.refs[index-1].Name, snapshot.refs[index].Name)
			}
		}
		for _, ref := range snapshot.refs {
			if err := CheckFormat(string(ref.Name), AllowOneLevel); err != nil {
				t.Fatalf("parsed name %q is invalid: %v", ref.Name, err)
			}
		}
		encoded := encodePackedRefs(snapshot.refs, snapshot.fullyPeeled)
		again, err := parsePackedRefs(encoded)
		if err != nil {
			t.Fatalf("parsePackedRefs of encoded data returned error %v", err)
		}
		if len(again.refs) != len(snapshot.refs) {
			t.Fatalf("round trip produced %d references instead of %d", len(again.refs), len(snapshot.refs))
		}
		for index, ref := range snapshot.refs {
			if again.refs[index] != ref {
				t.Fatalf("round trip produced %+v instead of %+v", again.refs[index], ref)
			}
		}
	})
}
