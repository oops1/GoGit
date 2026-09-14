//go:build oracle

package patch

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

type oracle struct {
	t   *testing.T
	dir string
	env []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	o := &oracle{t: t, dir: filepath.Join(root, "repo"), env: []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"HOME=" + home,
		"USERPROFILE=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
		"GIT_CONFIG_COUNT=4",
		"GIT_CONFIG_KEY_0=gc.auto",
		"GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=maintenance.auto",
		"GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=core.autocrlf",
		"GIT_CONFIG_VALUE_2=false",
		"GIT_CONFIG_KEY_3=core.eol",
		"GIT_CONFIG_VALUE_3=lf",
		"GIT_TERMINAL_PROMPT=0",
	}}
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	o.run(nil, "init", "-q", ".")
	return o
}

func (o *oracle) run(stdin []byte, args ...string) string {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = o.dir
	cmd.Env = o.env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		o.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, errs.String())
	}
	return out.String()
}

func (o *oracle) stagedAfterApplying(base string, hunks []diff.Hunk) string {
	o.t.Helper()
	if err := os.WriteFile(filepath.Join(o.dir, "f"), []byte(base), 0o600); err != nil {
		o.t.Fatal(err)
	}
	o.run(nil, "add", "f")
	var text bytes.Buffer
	file := diff.File{OldPath: "f", NewPath: "f", Status: diff.StatusModified, Hunks: hunks}
	if err := diff.Unified(&text, file, diff.Defaults()); err != nil {
		o.t.Fatal(err)
	}
	o.run(text.Bytes(), "apply", "--cached", "--unidiff-zero", "-")
	return o.run(nil, "cat-file", "-p", ":f")
}

type selection struct {
	name string
	old  string
	new  string
	pick func(hunks []diff.Hunk) Picked
}

func kindPicker(hunks []diff.Hunk, want diff.Kind, text string) Picked {
	return func(hunk, line int) bool {
		l := hunks[hunk].Lines[line]
		return l.Kind == want && l.Text == text
	}
}

func selections() []selection {
	long := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n"
	return []selection{
		{"everything", long, "1\nONE\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n19\n20\nTWENTY\n",
			func([]diff.Hunk) Picked { return All }},
		{"the second hunk only", long, "1\nONE\nUNO\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\nTWENTY\n",
			func([]diff.Hunk) Picked { return Hunk(1) }},
		{"the first hunk only", long, "1\nONE\nUNO\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\nTWENTY\n",
			func([]diff.Hunk) Picked { return Hunk(0) }},
		{"one added line of two", "a\nb\n", "a\nFIRST\nSECOND\nb\n",
			func(h []diff.Hunk) Picked { return kindPicker(h, diff.KindAdd, "SECOND") }},
		{"one deleted line of two", "a\nFIRST\nSECOND\nb\n", "a\nb\n",
			func(h []diff.Hunk) Picked { return kindPicker(h, diff.KindDel, "FIRST") }},
		{"the deletion of a replaced line", "a\nold\nb\n", "a\nnew\nb\n",
			func(h []diff.Hunk) Picked { return kindPicker(h, diff.KindDel, "old") }},
		{"the addition of a replaced line", "a\nold\nb\n", "a\nnew\nb\n",
			func(h []diff.Hunk) Picked { return kindPicker(h, diff.KindAdd, "new") }},
		{"a line added at the very top", "a\nb\n", "TOP\na\nb\nBOTTOM\n",
			func(h []diff.Hunk) Picked { return kindPicker(h, diff.KindAdd, "TOP") }},
		{"a line added at the very bottom", "a\nb\n", "TOP\na\nb\nBOTTOM\n",
			func(h []diff.Hunk) Picked { return kindPicker(h, diff.KindAdd, "BOTTOM") }},
		{"a file emptied", "a\nb\n", "",
			func([]diff.Hunk) Picked { return All }},
		{"a newline added at the end", "a", "a\n",
			func([]diff.Hunk) Picked { return All }},
	}
}

func TestOracleGitAppliesTheSelectedLinesTheWayWeDo(t *testing.T) {
	for _, s := range selections() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			hunks := diff.Blobs([]byte(s.old), []byte(s.new), diff.Defaults())

			selected, err := Select(hunks, s.pick(hunks))
			if err != nil {
				t.Fatalf("Select returned error %v", err)
			}
			ours, err := diff.Apply([]byte(s.old), selected)
			if err != nil {
				t.Fatalf("Apply returned error %v", err)
			}

			if git := o.stagedAfterApplying(s.old, selected); git != string(ours) {
				t.Fatalf("git staged %q, we staged %q", git, ours)
			}
		})
	}
}

func TestOracleGitDiscardsTheSelectedLinesTheWayWeDo(t *testing.T) {
	for _, s := range selections() {
		t.Run(s.name, func(t *testing.T) {
			o := newOracle(t)
			hunks := diff.Blobs([]byte(s.old), []byte(s.new), diff.Defaults())

			selected, err := Select(Reverse(hunks), s.pick(hunks))
			if err != nil {
				t.Fatalf("Select returned error %v", err)
			}
			ours, err := diff.Apply([]byte(s.new), selected)
			if err != nil {
				t.Fatalf("Apply returned error %v", err)
			}

			if git := o.stagedAfterApplying(s.new, selected); git != string(ours) {
				t.Fatalf("git left %q, we left %q", git, ours)
			}
		})
	}
}
