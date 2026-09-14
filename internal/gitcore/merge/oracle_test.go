//go:build oracle

package merge

import (
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type mergeCase struct {
	name   string
	base   []byte
	ours   []byte
	theirs []byte
}

func gitMergeFile(t *testing.T, c mergeCase, style Style, markerSize int) (string, bool) {
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
	if markerSize > 0 {
		args = append(args, "--marker-size="+strconv.Itoa(markerSize))
	}
	args = append(args, write("ours", c.ours), write("base", c.base), write("theirs", c.theirs))
	cmd := exec.Command("git", args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=gc.auto",
		"GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=maintenance.auto",
		"GIT_CONFIG_VALUE_1=false",
	}
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
		{"shared lines split a conflict", text("a", "b", "c"), text("a", "O1", "s1", "s2", "s3", "s4", "O2", "c"), text("a", "T1", "s1", "s2", "s3", "s4", "T2", "c")},
		{"a one-sided change keeps conflicts apart", text("a", "b", "c", "d", "e"), text("A", "b", "C", "d", "E"), text("A2", "b", "c", "d", "E2")},
		{"punctuation between conflicts", text("a", "{", "}", "", "}", ")", "g"), text("OURS", "{", "}", "", "}", ")", "OURS"), text("THEIRS", "{", "}", "", "}", ")", "THEIRS")},
		{"windows line endings", []byte("a\r\nb\r\nc\r\n"), []byte("a\r\nOURS\r\nc\r\n"), []byte("a\r\nTHEIRS\r\nc\r\n")},
		{"windows line endings without a final one", []byte("a\r\nb"), []byte("a\r\nOURS"), []byte("a\r\nTHEIRS")},
	}
}

func checkAgainstMergeFile(t *testing.T, c mergeCase, style Style, markerSize int) {
	t.Helper()
	want, conflicted := gitMergeFile(t, c, style, markerSize)
	got := File(c.base, c.ours, c.theirs, Options{
		Style:      style,
		Labels:     Labels{Ours: "ours", Base: "base", Theirs: "theirs"},
		MarkerSize: markerSize,
		Level:      LevelZealousAlnum,
	})
	if string(got.Content) != want {
		t.Fatalf("%s/%d/%d:\nbase %q\nours %q\ntheirs %q\n got %q\nwant %q", c.name, style, markerSize, c.base, c.ours, c.theirs, got.Content, want)
	}
	if conflicted != (got.Conflicts > 0) {
		t.Fatalf("%s/%d/%d: conflicts = %d, git %v", c.name, style, markerSize, got.Conflicts, conflicted)
	}
}

func TestOurMergeIsTheOneGitWrites(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	for _, c := range mergeCases() {
		for _, style := range []Style{StyleMerge, StyleDiff3, StyleZDiff3} {
			for _, markerSize := range []int{0, 3, 10} {
				checkAgainstMergeFile(t, c, style, markerSize)
			}
		}
	}
}

func randomText(r *rand.Rand, lines []string) []byte {
	eol := "\n"
	if r.IntN(6) == 0 {
		eol = "\r\n"
	}
	out := strings.Join(lines, eol)
	if len(lines) > 0 && r.IntN(5) != 0 {
		out += eol
	}
	return []byte(out)
}

func mutated(r *rand.Rand, base []string) []string {
	vocabulary := []string{"a", "b", "c", "x", "y", "{", "}", "", "shared", "OURS", "THEIRS"}
	var out []string
	for _, line := range base {
		switch r.IntN(8) {
		case 0:
		case 1:
			out = append(out, vocabulary[r.IntN(len(vocabulary))])
		case 2:
			out = append(out, line, vocabulary[r.IntN(len(vocabulary))])
		default:
			out = append(out, line)
		}
	}
	if r.IntN(4) == 0 {
		out = append(out, vocabulary[r.IntN(len(vocabulary))])
	}
	return out
}

func randomMergeCases(seed uint64, count int) []mergeCase {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	vocabulary := []string{"a", "b", "c", "d", "e", "f", "{", "}", "", "x"}
	cases := make([]mergeCase, 0, count)
	for at := range count {
		base := make([]string, r.IntN(14))
		for i := range base {
			base[i] = vocabulary[r.IntN(len(vocabulary))]
		}
		ours, theirs := mutated(r, base), mutated(r, base)
		cases = append(cases, mergeCase{
			name:   "random " + strconv.Itoa(at),
			base:   randomText(r, base),
			ours:   randomText(r, ours),
			theirs: randomText(r, theirs),
		})
	}
	return cases
}

func TestRandomMergesMatchGitMergeFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	for _, c := range randomMergeCases(20260914, 150) {
		for _, style := range []Style{StyleMerge, StyleDiff3, StyleZDiff3} {
			checkAgainstMergeFile(t, c, style, 0)
		}
	}
}
