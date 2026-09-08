//go:build oracle

package merge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type mergeCase struct {
	name   string
	base   []byte
	ours   []byte
	theirs []byte
}

func gitMergeFile(t *testing.T, c mergeCase, style Style) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	args := []string{"merge-file", "-p", "-L", "ours", "-L", "base", "-L", "theirs"}
	switch style {
	case StyleDiff3:
		args = append(args, "--diff3")
	case StyleZDiff3:
		args = append(args, "--zdiff3")
	}
	args = append(args, write("ours", c.ours), write("base", c.base), write("theirs", c.theirs))
	cmd := exec.Command("git", args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.Output()
	if err == nil {
		return string(out), false
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("git merge-file: %v", err)
	}
	return string(out), true
}

func mergeCases() []mergeCase {
	text := func(text ...string) []byte {
		return []byte(strings.Join(text, "\n") + "\n")
	}
	return []mergeCase{
		{"untouched", text("one", "two", "three"), text("one", "two", "three"), text("one", "two", "three")},
		{"ours only", text("a", "b", "c"), text("A", "b", "c"), text("a", "b", "c")},
		{"theirs only", text("a", "b", "c"), text("a", "b", "c"), text("a", "b", "C")},
		{"both ends", text("a", "b", "c", "d"), text("A", "b", "c", "d"), text("a", "b", "c", "D")},
		{"same change", text("a", "b", "c"), text("a", "B", "c"), text("a", "B", "c")},
		{"one place", text("a", "b", "c"), text("a", "OURS", "c"), text("a", "THEIRS", "c")},
		{"insert both", text("a", "b"), text("a", "ours", "b"), text("a", "theirs", "b")},
		{"delete ours", text("a", "b", "c"), text("a", "c"), text("a", "b", "c")},
		{"delete against change", text("a", "b", "c"), text("a", "c"), text("a", "B", "c")},
		{"delete both", text("a", "b", "c"), text("a", "c"), text("a", "c")},
		{"append ours", text("a", "b"), text("a", "b", "c"), text("a", "b")},
		{"append both", text("a", "b"), text("a", "b", "ours"), text("a", "b", "theirs")},
		{"prepend both", text("a", "b"), text("ours", "a", "b"), text("theirs", "a", "b")},
		{"shared line in conflict", text("a", "x", "b"), text("a", "shared", "OURS", "b"), text("a", "shared", "THEIRS", "b")},
		{"two conflicts", text("a", "b", "c", "d", "e"), text("A", "b", "c", "d", "E"), text("A1", "b", "c", "d", "E1")},
		{"adjacent changes", text("a", "b", "c"), text("A", "b", "c"), text("a", "B", "c")},
		{"whole file", text("a", "b"), text("x", "y"), text("p", "q")},
		{"empty base", nil, text("ours"), text("theirs")},
		{"empty ours", text("a", "b"), nil, text("a", "b", "c")},
		{"no trailing newline", []byte("a\nb"), []byte("a\nB"), []byte("a\nb")},
		{"conflict without trailing newline", []byte("a\nb"), []byte("a\nOURS"), []byte("a\nTHEIRS")},
		{"long block", text("1", "2", "3", "4", "5", "6"), text("1", "2", "X", "Y", "5", "6"), text("1", "2", "P", "Q", "5", "6")},
		{"common tail", text("x"), text("OURS", "shared"), text("THEIRS", "shared")},
		{"wide against narrow", text("a", "b", "c", "d"), text("A", "b", "C", "d"), text("X", "Y", "Z", "d")},
		{"common head and tail", text("x"), text("head", "OURS", "tail"), text("head", "THEIRS", "tail")},
	}
}

func TestOurMergeIsTheOneGitWrites(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	styles := []struct {
		name  string
		style Style
	}{
		{"merge", StyleMerge},
		{"diff3", StyleDiff3},
		{"zdiff3", StyleZDiff3},
	}
	for _, c := range mergeCases() {
		for _, style := range styles {
			want, conflicted := gitMergeFile(t, c, style.style)
			got := File(c.base, c.ours, c.theirs, Options{
				Style:  style.style,
				Labels: Labels{Ours: "ours", Base: "base", Theirs: "theirs"},
			})
			if string(got.Content) != want {
				t.Fatalf("%s/%s:\n got %q\nwant %q", c.name, style.name, got.Content, want)
			}
			if conflicted != (got.Conflicts > 0) {
				t.Fatalf("%s/%s: conflicts = %d, git %v", c.name, style.name, got.Conflicts, conflicted)
			}
		}
	}
}
