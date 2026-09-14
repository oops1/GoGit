package attributes

import (
	"strings"
	"testing"
)

func TestDecodeLFSPointerFollowsGitLFSSmudge(t *testing.T) {
	canonical := lfsPointerText(lfsTestOID, "12")
	tests := []struct {
		name string
		data string
		want LFSPointer
		ok   bool
	}{
		{"canonical", canonical, LFSPointer{OID: lfsTestOID, Size: 12}, true},
		{"surroundingSpace", "\n " + strings.ReplaceAll(canonical, "\n", "\r\n") + "  ", LFSPointer{OID: lfsTestOID, Size: 12}, true},
		{"legacy", "version https://hawser.github.com/spec/v1\noid sha256:" + lfsTestOID + "\nsize 0\n", LFSPointer{OID: lfsTestOID}, true},
		{"extension", "version " + lfsSpecURL + "\next-0-foo sha256:" + lfsTestOID + "\noid sha256:" + lfsTestOID + "\nsize 3\n", LFSPointer{OID: lfsTestOID, Size: 3, Extended: true}, true},
		{"noMarker", "version x\noid sha256:" + lfsTestOID + "\nsize 3\n", LFSPointer{}, false},
		{"tooLarge", canonical + strings.Repeat(" ", lfsPointerLimit), LFSPointer{}, false},
		{"invalid", "git-lfs", LFSPointer{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DecodeLFSPointer([]byte(tc.data))
			if got != tc.want || ok != tc.ok {
				t.Fatalf("DecodeLFSPointer = %+v, %v, want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLFSPointerEncodeWritesTheCurrentSpec(t *testing.T) {
	if got, want := string(LFSPointer{OID: lfsTestOID, Size: 5}.Encode()), lfsPointerText(lfsTestOID, "5"); got != want {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
	if got := (LFSPointer{OID: lfsTestOID}).Encode(); got != nil {
		t.Fatalf("Encode of an empty object = %q, want nothing", got)
	}
}

func TestPolicyClassifiesSmudgeDrivers(t *testing.T) {
	attrs := map[string]string{".gitattributes": "*.lfs filter=lfs\n*.ext filter=ext\n"}
	tests := []struct {
		name        string
		config      string
		path        string
		clean       FilterKind
		smudge      FilterKind
		skip        bool
		unsupported bool
	}{
		{"noConfig", "", "a.lfs", FilterNone, FilterNone, false, false},
		{"lfsProcess", lfsDriverConfig, "a.lfs", FilterLFS, FilterLFS, false, false},
		{"lfsSkipProcess", "[filter \"lfs\"]\n\tprocess = git-lfs filter-process --skip\n", "a.lfs", FilterLFS, FilterLFS, true, false},
		{"lfsSmudgeOnly", "[filter \"lfs\"]\n\tsmudge = git-lfs smudge -- %f\n", "a.lfs", FilterNone, FilterLFS, false, false},
		{"lfsSkipSmudge", "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge --skip -- %f\n", "a.lfs", FilterLFS, FilterLFS, true, false},
		{"lfsExtension", lfsDriverConfig + "[lfs \"extension.foo\"]\n\tsmudge = foo\n", "a.lfs", FilterExternal, FilterExternal, false, true},
		{"externalSmudge", "[filter \"ext\"]\n\tsmudge = indent\n", "a.ext", FilterNone, FilterExternal, false, true},
		{"externalClean", "[filter \"ext\"]\n\tclean = indent\n", "a.ext", FilterExternal, FilterNone, false, true},
		{"smudgeCommandAsClean", "[filter \"ext\"]\n\tclean = git-lfs smudge -- %f\n", "a.ext", FilterExternal, FilterNone, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := AttributeOptions{}
			if tc.config != "" {
				opts.Config = loadConfig(t, tc.config)
			}
			policy := testAttributes(attrs, opts).Policy(tc.path)
			if policy.Clean != tc.clean || policy.Smudge != tc.smudge || policy.SkipSmudge != tc.skip || policy.FilterUnsupported() != tc.unsupported {
				t.Fatalf("Policy(%q) = clean %d smudge %d skip %v unsupported %v", tc.path, policy.Clean, policy.Smudge, policy.SkipSmudge, policy.FilterUnsupported())
			}
		})
	}
}

func TestLFSFetchFilterFollowsIncludeAndExcludeLists(t *testing.T) {
	if !NewLFSFetchFilter(nil, false).Allows("a.bin") {
		t.Fatal("a filter without configuration refuses a path")
	}
	tests := []struct {
		name   string
		config string
		icase  bool
		path   string
		want   bool
	}{
		{"noSettings", "", false, "a.bin", true},
		{"blankSettings", "[lfs]\n\tfetchinclude = \"  \"\n", false, "a.bin", true},
		{"excludedDirectory", "[lfs]\n\tfetchexclude = sub\n", false, "sub/c.bin", false},
		{"excludedNestedDirectory", "[lfs]\n\tfetchexclude = sub/\n", false, "x/sub/c.bin", false},
		{"notExcluded", "[lfs]\n\tfetchexclude = sub\n", false, "b.bin", true},
		{"excludedByName", "[lfs]\n\tfetchexclude = *.bin\n", false, "sub/c.bin", false},
		{"excludeWinsOverInclude", "[lfs]\n\tfetchinclude = sub\n\tfetchexclude = *.bin\n", false, "sub/c.bin", false},
		{"notIncluded", "[lfs]\n\tfetchinclude = sub\n", false, "b.bin", false},
		{"includedFromList", "[lfs]\n\tfetchinclude = docs, sub\\\\\n", false, "sub/c.bin", true},
		{"anchoredPath", "[lfs]\n\tfetchexclude = /media/raw\n", false, "media/raw/a.bin", false},
		{"caseFolded", "[lfs]\n\tfetchexclude = SUB\n", true, "sub/c.bin", false},
		{"caseKept", "[lfs]\n\tfetchexclude = SUB\n", false, "sub/c.bin", true},
		{"emptyPatternMatchesNothing", "[lfs]\n\tfetchinclude = \",\"\n", false, "a/b.bin", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter := NewLFSFetchFilter(loadConfig(t, tc.config), tc.icase)
			if got := filter.Allows(tc.path); got != tc.want {
				t.Fatalf("Allows(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
	if got := lfsPathPatterns("a/,b"); len(got) != 2 || got[0].text != "a" || !got[0].noDir {
		t.Fatalf("lfsPathPatterns = %+v", got)
	}
}
