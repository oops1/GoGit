//go:build oracle

package worktree

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func (o *oracle) ageFile(dir, rel string) {
	o.t.Helper()
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(rel)), past, past); err != nil {
		o.t.Fatalf("Chtimes returned error %v", err)
	}
}

func TestOracleStatusOfConvertedFilesMatchesGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("converted")
	o.run(dir, "init", "-q", "-b", "main", ".")
	if _, err := o.attempt(dir, "lfs", "install", "--local"); err != nil {
		t.Skipf("git-lfs is not available: %v", err)
	}
	o.run(dir, "config", "core.autocrlf", "false")
	o.write(dir, ".gitattributes", "*.bin filter=lfs -text\n*.c ident\n")
	o.write(dir, "same.bin", "same\x00payload")
	o.write(dir, "changed.bin", "old\x00payload")
	o.write(dir, "crlf.txt", "one\r\ntwo\r\n")
	o.write(dir, "id.c", "$Id$\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "converted")
	o.run(dir, "config", "core.autocrlf", "true")
	o.write(dir, "changed.bin", "new\x00payload")
	for _, rel := range []string{"same.bin", "crlf.txt", "id.c"} {
		o.ageFile(dir, rel)
	}

	ours := entryMap(o.ourStatus(dir).Entries)
	changed, _ := o.status(dir)
	for _, rel := range []string{"same.bin", "changed.bin", "crlf.txt", "id.c"} {
		gitEntry, gitChanged := changed[rel]
		ourEntry, ourChanged := ours[rel]
		if gitChanged != ourChanged {
			t.Errorf("%s: git reports %+v (%v), we report %+v (%v)", rel, gitEntry, gitChanged, ourEntry, ourChanged)
		}
		if gitChanged && (gitEntry.y == 'M') != (ourEntry.Unstaged == StatusModified) {
			t.Errorf("%s: git reports %c in the worktree, we report %v", rel, gitEntry.y, ourEntry.Unstaged)
		}
	}
}
