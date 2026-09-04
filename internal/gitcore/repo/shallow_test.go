package repo

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func mustParseHex(t *testing.T, text string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(text)
	if err != nil {
		t.Fatalf("hash.Parse(%q) returned error %v", text, err)
	}
	return id
}

func TestShallowReturnsEmptySetWhenFileIsMissing(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))

	repository := openRepo(t, work, openOptions(t, env{}))
	shallow, err := repository.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	if len(shallow) != 0 {
		t.Fatalf("Shallow returned %v, want an empty set", shallow)
	}
}

func TestShallowParsesListedCommits(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	one := "1111111111111111111111111111111111111111"
	two := "2222222222222222222222222222222222222222"
	writeFile(t, filepath.Join(gitDir, shallowFileName), "\n"+one+"\n\n"+two+"\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	shallow, err := repository.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	want := map[hash.ObjectID]struct{}{
		mustParseHex(t, one): {},
		mustParseHex(t, two): {},
	}
	if len(shallow) != len(want) {
		t.Fatalf("Shallow returned %d entries, want %d", len(shallow), len(want))
	}
	for id := range want {
		if _, ok := shallow[id]; !ok {
			t.Errorf("Shallow is missing %s", id)
		}
	}
}

func TestShallowParsesFileWithoutTrailingNewline(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	one := "3333333333333333333333333333333333333333"
	writeFile(t, filepath.Join(gitDir, shallowFileName), one)

	repository := openRepo(t, work, openOptions(t, env{}))
	shallow, err := repository.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	if _, ok := shallow[mustParseHex(t, one)]; !ok || len(shallow) != 1 {
		t.Fatalf("Shallow returned %v, want only %s", shallow, one)
	}
}

func TestShallowFailsWhenTheFileIsADirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	makeDir(t, filepath.Join(gitDir, shallowFileName))

	repository := openRepo(t, work, openOptions(t, env{}))
	if _, err := repository.Shallow(); err == nil {
		t.Fatal("Shallow returned no error for a directory named shallow")
	}
}

func TestShallowRejectsAnOverlongLine(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	writeFile(t, filepath.Join(gitDir, shallowFileName), strings.Repeat("a", 1<<20)+"\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	if _, err := repository.Shallow(); !errors.Is(err, ErrInvalidShallowFile) {
		t.Fatalf("Shallow returned %v, want %v", err, ErrInvalidShallowFile)
	}
}

func TestShallowRejectsAMalformedLine(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	writeFile(t, filepath.Join(gitDir, shallowFileName), "not-a-hex-id\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	if _, err := repository.Shallow(); !errors.Is(err, ErrInvalidShallowFile) {
		t.Fatalf("Shallow returned %v, want %v", err, ErrInvalidShallowFile)
	}
}
