package revision

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func FuzzParse(f *testing.F) {
	b := parseFixture(f)
	ctx := b.context()
	ctx.Config = pushConfig{fakeConfig: fakeConfig{
		upstream: map[string]string{"main": "refs/remotes/origin/main"},
		push:     map[string]string{"main": "refs/remotes/origin/main"},
	}}
	seeds := []string{
		"",
		"@",
		"HEAD",
		"main",
		"refs/heads/main",
		"v2",
		"v2^{}",
		"v2^{commit}",
		"main^{tree}",
		"HEAD^",
		"HEAD^2",
		"HEAD~2",
		"HEAD^0",
		"HEAD@{0}",
		"main@{1}",
		"@{-1}",
		"@{u}",
		"main@{push}",
		":/c2",
		"HEAD^{/c4}",
		"HEAD:file.txt",
		"HEAD:dir/nested.txt",
		"HEAD:",
		":0:file.txt",
		"@{yesterday}",
		b.id("c1").String(),
		b.id("c1").String()[:7],
		"^{}",
		"~",
		"@{",
		"}{@",
		"main@{-1}",
		":/!",
		"a..b",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, spec string) {
		rev, err := Parse(spec, ctx)
		if err == nil {
			return
		}
		if rev.ID != hash.Zero || rev.Ref != "" {
			t.Fatalf("Parse(%q) failed with %v but returned %+v", spec, err, rev)
		}
	})
}
