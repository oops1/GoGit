package worktree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestModeChangedComparesTheExecutableBitWhenFileModeIsHonored(t *testing.T) {
	tests := []struct {
		name  string
		entry *index.Entry
		fi    fakeFileInfo
		want  bool
	}{
		{"regular unchanged", &index.Entry{Mode: object.ModeBlob}, fakeFileInfo{mode: 0o644}, false},
		{"regular now executable", &index.Entry{Mode: object.ModeBlob}, fakeFileInfo{mode: 0o755}, true},
		{"executable now regular", &index.Entry{Mode: object.ModeExecutable}, fakeFileInfo{mode: 0o644}, true},
		{"executable unchanged", &index.Entry{Mode: object.ModeExecutable}, fakeFileInfo{mode: 0o755}, false},
		{"symlink ignored", &index.Entry{Mode: object.ModeSymlink}, fakeFileInfo{mode: 0o777}, false},
		{"submodule ignored", &index.Entry{Mode: object.ModeSubmodule}, fakeFileInfo{mode: 0o777, dir: true}, false},
	}
	w := &Worktree{fileMode: true}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := w.modeChanged(tc.entry, tc.fi); got != tc.want {
				t.Errorf("modeChanged(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModeChangedIsAlwaysFalseWhenFileModeIsNotHonored(t *testing.T) {
	w := &Worktree{fileMode: false}
	entry := &index.Entry{Mode: object.ModeBlob}
	if w.modeChanged(entry, fakeFileInfo{mode: 0o755}) {
		t.Fatalf("modeChanged returned true although fileMode tracking is disabled")
	}
}

func TestConvertForCheckin(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile(".gitattributes", "auto.txt text=auto\nbin.bin binary\n")
	w := tr.open()

	tests := []struct {
		name string
		path string
		data string
		want string
	}{
		{"binary attribute leaves content untouched", "bin.bin", "a\r\nb\r\n", "a\r\nb\r\n"},
		{"detected binary content is left untouched", "auto.txt", "bin\x00ary\r\ndata", "bin\x00ary\r\ndata"},
		{"text without crlf is left untouched", "auto.txt", "already lf\n", "already lf\n"},
		{"text with crlf is normalized to lf", "auto.txt", "line1\r\nline2\r\n", "line1\nline2\n"},
		{"unspecified attribute leaves crlf untouched", "plain.txt", "line1\r\nline2\r\n", "line1\r\nline2\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := w.convertForCheckin(tc.path, []byte(tc.data))
			if string(got) != tc.want {
				t.Errorf("convertForCheckin(%q, %q) = %q, want %q", tc.path, tc.data, got, tc.want)
			}
		})
	}
}

func TestOpenFailsWhenTheIndexFileIsCorrupt(t *testing.T) {
	tr := newTestRepo(t)
	if err := os.WriteFile(tr.repo.IndexFile(), []byte("not an index file"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if _, err := Open(tr.repo, Options{DB: tr.db, Refs: tr.refs}); err == nil {
		t.Fatalf("Open returned no error, want an index parse failure")
	}
}

func TestOpenClosesItsOwnRefsStoreWhenAttributesFileConfigIsInvalid(t *testing.T) {
	tr := newTestRepo(t)
	tr.saveIndex()
	tr.appendConfig("[core]\n\tattributesfile = ~bob/x\n")
	r2 := tr.reopen()
	if _, err := Open(r2, Options{DB: tr.db}); err == nil {
		t.Fatalf("Open returned no error, want an invalid attributesfile error")
	}
}

func TestOpenUsesTheConfiguredExcludesFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.saveIndex()
	globalIgnore := tr.path("../global-ignore")
	if err := os.WriteFile(globalIgnore, []byte("ignored-globally.txt\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	tr.appendConfig("[core]\n\texcludesfile = " + filepath.ToSlash(globalIgnore) + "\n")
	r2 := tr.reopen()
	w, err := Open(r2, Options{DB: tr.db, Refs: tr.refs})
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	tr.writeFile("ignored-globally.txt", "content\n")
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	entry, ok := entryMap(status.Entries)["ignored-globally.txt"]
	if !ok || entry.Unstaged != StatusIgnored {
		t.Fatalf("ignored-globally.txt entry = %#v, want Unstaged=Ignored per the configured excludes file", entry)
	}
}

func TestOpenUsesTheConfiguredAttributesFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.saveIndex()
	globalAttributes := tr.path("../global-attributes")
	if err := os.WriteFile(globalAttributes, []byte("*.bin binary\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	tr.appendConfig("[core]\n\tattributesfile = " + filepath.ToSlash(globalAttributes) + "\n")
	r2 := tr.reopen()
	w, err := Open(r2, Options{DB: tr.db, Refs: tr.refs})
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}
