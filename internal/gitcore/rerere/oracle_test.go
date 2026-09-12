//go:build oracle

package rerere

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".no-global"),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=maintenance.auto", "GIT_CONFIG_VALUE_1=false",
		"GIT_AUTHOR_NAME=oracle", "GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_COMMITTER_NAME=oracle", "GIT_COMMITTER_EMAIL=oracle@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0000", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func tryGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".no-global"),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=maintenance.auto", "GIT_CONFIG_VALUE_1=false",
		"GIT_AUTHOR_NAME=oracle", "GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_COMMITTER_NAME=oracle", "GIT_COMMITTER_EMAIL=oracle@example.com",
		"GIT_AUTHOR_DATE=1700000000 +0000", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func tenLines(seed string) string {
	var out []string
	for i := range 10 {
		out = append(out, seed+" line "+strconv.Itoa(i)+" long enough to be recognised")
	}
	return strings.Join(out, "\n") + "\n"
}

func changeLine(text string, line int, replacement string) string {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	parts[line] = replacement
	return strings.Join(parts, "\n") + "\n"
}

type oracleCase struct {
	name       string
	ours       map[string]string
	theirs     map[string]string
	style      string
	markerSize int
	reverse    bool
}

func conflictCases() []oracleCase {
	f := tenLines("f")
	return []oracleCase{
		{name: "one hunk", ours: map[string]string{"f": changeLine(f, 4, "OURS")}, theirs: map[string]string{"f": changeLine(f, 4, "THEIRS")}},
		{name: "two hunks", ours: map[string]string{"f": changeLine(changeLine(f, 1, "OURS ONE"), 8, "OURS TWO")},
			theirs: map[string]string{"f": changeLine(changeLine(f, 1, "THEIRS ONE"), 8, "THEIRS TWO")}},
		{name: "added differently", ours: map[string]string{"new": "ours\n"}, theirs: map[string]string{"new": "theirs\n"}},
		{name: "the same conflict from the other side", ours: map[string]string{"f": changeLine(f, 4, "OURS")},
			theirs: map[string]string{"f": changeLine(f, 4, "THEIRS")}, reverse: true},
		{name: "diff3 style", ours: map[string]string{"f": changeLine(f, 4, "OURS")},
			theirs: map[string]string{"f": changeLine(f, 4, "THEIRS")}, style: "diff3"},
		{name: "zdiff3 style", ours: map[string]string{"f": changeLine(f, 4, "OURS")},
			theirs: map[string]string{"f": changeLine(f, 4, "THEIRS")}, style: "zdiff3"},
		{name: "wide markers", ours: map[string]string{"f": changeLine(f, 4, "OURS")},
			theirs: map[string]string{"f": changeLine(f, 4, "THEIRS")}, markerSize: 11},
		{name: "a file without a trailing newline", ours: map[string]string{"f": "one\ntwo\nOURS"}, theirs: map[string]string{"f": "one\ntwo\nTHEIRS"}},
	}
}

func buildConflict(t *testing.T, c oracleCase) (dir string, paths []string) {
	t.Helper()
	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main", ".")
	runGit(t, dir, "config", "rerere.enabled", "true")
	runGit(t, dir, "config", "core.autocrlf", "false")
	if c.style != "" {
		runGit(t, dir, "config", "merge.conflictStyle", c.style)
	}
	if c.markerSize > 0 {
		write(t, dir, ".gitattributes", "* conflict-marker-size="+strconv.Itoa(c.markerSize)+"\n")
	}
	write(t, dir, "f", tenLines("f"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "branch", "feature")
	for rel, content := range c.ours {
		write(t, dir, rel, content)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "ours")
	runGit(t, dir, "checkout", "-q", "feature")
	for rel, content := range c.theirs {
		write(t, dir, rel, content)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "theirs")
	from, into := "main", "feature"
	if !c.reverse {
		runGit(t, dir, "checkout", "-q", "main")
		from, into = "feature", "main"
	}
	t.Logf("merging %s into %s", from, into)
	if _, err := tryGit(t, dir, "merge", "--no-edit", from); err == nil {
		t.Fatal("the merge did not conflict")
	}
	for line := range strings.SplitSeq(runGit(t, dir, "diff", "--name-only", "--diff-filter=U"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return dir, paths
}

func recordedIDs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".git", "rr-cache"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, entry := range entries {
		ids = append(ids, entry.Name())
	}
	return ids
}

func TestOracleConflictIDsMatchTheOnesGitRecords(t *testing.T) {
	for _, c := range conflictCases() {
		t.Run(c.name, func(t *testing.T) {
			dir, paths := buildConflict(t, c)
			if len(paths) != 1 {
				t.Fatalf("conflicted paths = %v", paths)
			}
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(paths[0])))
			if err != nil {
				t.Fatal(err)
			}

			conflict, ok := Normalize(data, c.markerSize)

			if !ok {
				t.Fatalf("the file was not seen as conflicted:\n%s", data)
			}
			if ids := recordedIDs(t, dir); len(ids) != 1 || ids[0] != conflict.ID {
				t.Fatalf("id = %s, git recorded %v", conflict.ID, ids)
			}
			preimage, err := os.ReadFile(filepath.Join(dir, ".git", "rr-cache", conflict.ID, "preimage"))
			if err != nil {
				t.Fatal(err)
			}
			if string(preimage) != string(conflict.Preimage) {
				t.Fatalf("preimage =\n%q\nwant\n%q", conflict.Preimage, preimage)
			}
		})
	}
}

func TestOracleTheSameResolutionIsRecognisedFromEitherSide(t *testing.T) {
	f := tenLines("f")
	straight := oracleCase{ours: map[string]string{"f": changeLine(f, 4, "OURS")}, theirs: map[string]string{"f": changeLine(f, 4, "THEIRS")}}
	reversed := straight
	reversed.reverse = true

	first, _ := buildConflict(t, straight)
	second, _ := buildConflict(t, reversed)

	if one, two := recordedIDs(t, first), recordedIDs(t, second); len(one) != 1 || len(two) != 1 || one[0] != two[0] {
		t.Fatalf("ids = %v and %v", one, two)
	}
}
