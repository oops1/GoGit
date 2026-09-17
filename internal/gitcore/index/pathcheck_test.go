package index

import (
	"errors"
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestVerifyPathFollowsGitRules(t *testing.T) {
	plain := PathRules{}
	ntfs := PathRules{ProtectNTFS: true}
	hfs := PathRules{ProtectHFS: true}
	windows := PathRules{ProtectNTFS: true, Windows: true}
	windowsWithoutNTFS := PathRules{Windows: true}
	tests := []struct {
		name  string
		rules PathRules
		path  string
		mode  object.Mode
		want  bool
	}{
		{"nested file", plain, "a/b", object.ModeBlob, true},
		{"empty path", plain, "", object.ModeBlob, false},
		{"empty directory path", plain, "", object.ModeTree, true},
		{"directory with trailing slash", plain, "a/", object.ModeTree, true},
		{"file with trailing slash", plain, "a/", object.ModeBlob, false},
		{"doubled slash", plain, "a//b", object.ModeBlob, false},
		{"leading slash", plain, "/a", object.ModeBlob, false},
		{"dot", plain, ".", object.ModeBlob, false},
		{"dot inside", plain, "a/./b", object.ModeBlob, false},
		{"dot dot", plain, "..", object.ModeBlob, false},
		{"dot dot inside", plain, "a/../b", object.ModeBlob, false},
		{"dot dot prefix", plain, "..x", object.ModeBlob, true},
		{"dotfile", plain, ".x", object.ModeBlob, true},
		{"dot git", plain, ".git", object.ModeBlob, false},
		{"upper dot git directory", plain, ".GIT/config", object.ModeBlob, false},
		{"nested dot git", plain, "a/.Git", object.ModeBlob, false},
		{"dot git prefix", plain, ".gitx", object.ModeBlob, true},
		{"dot gi", plain, ".gi", object.ModeBlob, true},
		{"dot gx", plain, ".gxt", object.ModeBlob, true},
		{"dot g", plain, ".g", object.ModeBlob, true},
		{"gitmodules symlink", plain, ".gitmodules", object.ModeSymlink, false},
		{"gitmodules file", plain, ".gitmodules", object.ModeBlob, true},
		{"gitmodules symlink prefix", plain, ".GITMODULES/x", object.ModeSymlink, false},
		{"gitmodules symlink suffix", plain, ".gitmodulesx", object.ModeSymlink, true},
		{"git symlink other suffix", plain, ".gitattributes", object.ModeSymlink, true},
		{"short name without ntfs", plain, "git~1/config", object.ModeBlob, true},
		{"short name", ntfs, "git~1/config", object.ModeBlob, false},
		{"upper short name", ntfs, "GIT~1", object.ModeBlob, false},
		{"trailing dot and space", ntfs, ".git ./x", object.ModeBlob, false},
		{"trailing dot", ntfs, ".git.", object.ModeBlob, false},
		{"stream", ntfs, ".git:stream", object.ModeBlob, false},
		{"backslash separator after", ntfs, ".git\\x", object.ModeBlob, false},
		{"ntfs dot git prefix", ntfs, ".gitx", object.ModeBlob, true},
		{"ntfs dot git space word", ntfs, ".git x", object.ModeBlob, true},
		{"other short name", ntfs, "git~2", object.ModeBlob, true},
		{"git word", ntfs, "gitx", object.ModeBlob, true},
		{"short dot", ntfs, ".gi", object.ModeBlob, true},
		{"backslash component", ntfs, "a\\.git", object.ModeBlob, false},
		{"backslash component without ntfs", plain, "a\\.git", object.ModeBlob, true},
		{"backslash harmless", ntfs, "a\\b", object.ModeBlob, true},
		{"gitmodules trailing", ntfs, ".gitmodules .", object.ModeSymlink, false},
		{"gitmodules stream", ntfs, ".gitmodules:x", object.ModeSymlink, false},
		{"gitmodules word", ntfs, ".gitmodules x", object.ModeSymlink, true},
		{"gitmodules short name", ntfs, "gitmod~1", object.ModeSymlink, false},
		{"gitmodules upper short name", ntfs, "GITMOD~4", object.ModeSymlink, false},
		{"gitmodules short name word", ntfs, "gitmod~1 x", object.ModeSymlink, true},
		{"gitmodules short name five", ntfs, "gitmod~5", object.ModeSymlink, true},
		{"gitmodules fallback", ntfs, "gi7eba~1", object.ModeSymlink, false},
		{"gitmodules fallback two digits", ntfs, "gi7eb~12", object.ModeSymlink, false},
		{"gitmodules fallback letter", ntfs, "gi7e~9x", object.ModeSymlink, true},
		{"gitmodules fallback short", ntfs, "gi7eba", object.ModeSymlink, true},
		{"gitmodules fallback seventh letter", ntfs, "gi7ebax1", object.ModeSymlink, true},
		{"gitmodules fallback non ascii", ntfs, "g\xc3\xa9t", object.ModeSymlink, true},
		{"gitmodules fallback zero", ntfs, "gi7eba~0", object.ModeSymlink, true},
		{"gitmodules fallback tilde at end", ntfs, "gi7eb~", object.ModeSymlink, true},
		{"gitmodules fallback trailing word", ntfs, "gi7eba~1x", object.ModeSymlink, true},
		{"gitmodules fallback as file", ntfs, "gi7eba~1", object.ModeBlob, true},
		{"hfs joiner", hfs, ".g\u200cit", object.ModeBlob, false},
		{"hfs upper with joiner", hfs, ".\u200dGIT/x", object.ModeBlob, false},
		{"hfs without protection", plain, ".gi\u200et", object.ModeBlob, true},
		{"hfs prefix", hfs, ".gitx", object.ModeBlob, true},
		{"hfs malformed inside", hfs, ".g\xffit", object.ModeBlob, true},
		{"hfs malformed after", hfs, ".git\xff", object.ModeBlob, false},
		{"hfs malformed first", hfs, "\xff.git", object.ModeBlob, true},
		{"hfs accent", hfs, ".gi\u00e9", object.ModeBlob, true},
		{"hfs noncharacter", hfs, ".git\uffff", object.ModeBlob, false},
		{"hfs gitmodules symlink", hfs, ".gitmodules\u200c", object.ModeSymlink, false},
		{"hfs byte order mark", hfs, ".\ufeffgit", object.ModeBlob, false},
		{"hfs nested joiner", hfs, "a/.git\u200c/x", object.ModeBlob, false},
		{"hfs letter after", hfs, ".gitA", object.ModeBlob, true},
		{"hfs literal replacement", hfs, ".git\ufffd", object.ModeBlob, true},
		{"drive prefix", windows, "C:/x", object.ModeBlob, false},
		{"drive prefix without ntfs", windowsWithoutNTFS, "c:x", object.ModeBlob, false},
		{"windows backslash", windows, "a\\b", object.ModeBlob, false},
		{"windows backslash without ntfs", windowsWithoutNTFS, "a\\b", object.ModeBlob, true},
		{"windows backslash dot git without ntfs", windowsWithoutNTFS, "a\\.git", object.ModeBlob, false},
		{"windows hfs backslash", PathRules{ProtectHFS: true, Windows: true}, ".git\u200c\\x", object.ModeBlob, false},
		{"device", windows, "con", object.ModeBlob, false},
		{"device with extension", windows, "CON.txt", object.ModeBlob, false},
		{"nested device with space", windows, "a/nul .txt", object.ModeBlob, false},
		{"device with word", windows, "nul x", object.ModeBlob, true},
		{"com port", windows, "com1", object.ModeBlob, false},
		{"com zero", windows, "COM0", object.ModeBlob, true},
		{"lpt with extension", windows, "lpt9.x", object.ModeBlob, false},
		{"lpt letter", windows, "lptx", object.ModeBlob, true},
		{"lpt bare", windows, "lpt", object.ModeBlob, true},
		{"conin", windows, "CONIN$", object.ModeBlob, false},
		{"conout", windows, "conout$.log", object.ModeBlob, false},
		{"console", windows, "console", object.ModeBlob, true},
		{"device without ntfs", windowsWithoutNTFS, "aux", object.ModeBlob, true},
		{"colon", windows, "a/b:c", object.ModeBlob, false},
		{"angle", windows, "a<b", object.ModeBlob, false},
		{"pipe", windows, "a|b", object.ModeBlob, false},
		{"control", windows, "a\x01b", object.ModeBlob, false},
		{"trailing period", windows, "a.", object.ModeBlob, false},
		{"trailing space", windows, "a /b", object.ModeBlob, false},
		{"high byte", windows, "abc\x80", object.ModeBlob, true},
		{"windows nested", windows, "dir/file.txt", object.ModeBlob, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyPath(tc.path, tc.mode, tc.rules)
			if tc.want && err != nil {
				t.Fatalf("VerifyPath(%q) returned error %v", tc.path, err)
			}
			if !tc.want && !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("VerifyPath(%q) = %v, want ErrUnsafePath", tc.path, err)
			}
		})
	}
}

func TestValidWin32PathKeepsDotAndDotDotSegments(t *testing.T) {
	for path, want := range map[string]bool{".": true, "..": true, "a/./..": true, "...": false, "a//b": true} {
		if got := ValidWin32Path(path); got != want {
			t.Fatalf("ValidWin32Path(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestDefaultPathRulesProtectNTFSEverywhere(t *testing.T) {
	rules := DefaultPathRules()
	if !rules.ProtectNTFS {
		t.Fatal("NTFS protection is off by default")
	}
	if rules.Windows != (runtime.GOOS == "windows") || rules.ProtectHFS != (runtime.GOOS == "darwin") {
		t.Fatalf("rules = %+v on %s", rules, runtime.GOOS)
	}
}

func TestAddVerifiedRefusesUnsafePaths(t *testing.T) {
	idx := New(Version2)
	if err := idx.AddVerified(blobEntry(".git/config", StageMerged), DefaultPathRules()); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("AddVerified = %v, want ErrUnsafePath", err)
	}
	if idx.Len() != 0 {
		t.Fatalf("the index holds %d entries", idx.Len())
	}
	if err := idx.AddVerified(blobEntry("a/b", StageMerged), DefaultPathRules()); err != nil {
		t.Fatalf("AddVerified returned error %v", err)
	}
	if idx.Len() != 1 {
		t.Fatalf("the index holds %d entries", idx.Len())
	}
}
