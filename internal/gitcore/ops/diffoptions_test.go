package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

func TestDetailsTreatAFileMarkedMinusDiffAsBinary(t *testing.T) {
	tr := newTestRepo(t)
	tr.commitFiles("base", map[string]string{".gitattributes": "*.dat -diff\n", "x.dat": "one\n", "y.txt": "one\n"})
	tr.commitFiles("edit", map[string]string{"x.dat": "two\n", "y.txt": "two\n"})

	result, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	if len(result.Changes) != 2 || !result.Changes[0].Binary || result.Changes[1].Binary {
		t.Fatalf("changes = %+v", result.Changes)
	}
}

func TestDetailsTakeHunkHeadersFromTheDiffDriverOfEachPath(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[diff \"default\"]\n\txfuncname = ^(section .*)$\n[diff \"broken\"]\n\txfuncname = (\n")
	tr.repo = tr.reopen()
	body := "section one\nfunc helper() {\ndef method(self):\n\tone\n\ttwo\n\tthree\n\tfour\n\tfive\n"
	edited := strings.Replace(body, "five", "FIVE", 1)
	rules := "*.py diff=python\n*.set diff\n*.named diff=nosuch\n*.bad diff=broken\n*.go diff=golang\n"
	names := []string{"a.py", "b.set", "c.named", "d.bad", "e.go", "f.txt"}
	base, edit := map[string]string{".gitattributes": rules}, map[string]string{}
	for _, name := range names {
		base[name], edit[name] = body, edited
	}
	tr.commitFiles("base", base)
	tr.commitFiles("edit", edit)

	result, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{})

	if err != nil {
		t.Fatalf("Details returned error %v", err)
	}
	want := map[string]string{
		"a.py":    "def method(self):",
		"b.set":   "def method(self):",
		"c.named": "section one",
		"d.bad":   "def method(self):",
		"e.go":    "func helper() {",
		"f.txt":   "section one",
	}
	for _, file := range result.Changes {
		if len(file.Hunks) != 1 || file.Hunks[0].Header != want[file.NewPath] {
			t.Errorf("%s hunks = %+v", file.NewPath, file.Hunks)
		}
	}
}

func TestRepoDiffOptionsReadTheConfiguredAlgorithm(t *testing.T) {
	tr := newTestRepo(t)
	tr.appendConfig("[diff]\n\talgorithm = Histogram\n")

	opts, err := repoDiffOptions(tr.reopen(), diff.Options{})

	if err != nil || opts.Algorithm != diff.AlgorithmHistogram || opts.BinaryHint == nil || opts.RenameThreshold != diff.DefaultRenameThreshold {
		t.Fatalf("options = %+v, %v", opts, err)
	}
}

func TestRepoDiffOptionsKeepTheCallersChoices(t *testing.T) {
	tr := newBareTestRepo(t)
	tr.appendConfig("[diff]\n\talgorithm = patience\n")
	hint := func(string) (bool, bool) { return true, true }

	kept, err := repoDiffOptions(tr.reopen(), diff.Options{RenameThreshold: 70, BinaryHint: hint})
	if err != nil || kept.Algorithm != diff.AlgorithmMyers || kept.RenameThreshold != 70 {
		t.Fatalf("options = %+v, %v", kept, err)
	}
	bare, err := repoDiffOptions(tr.reopen(), diff.Options{RenameThreshold: 70})
	if err != nil || bare.BinaryHint == nil {
		t.Fatalf("options = %+v, %v", bare, err)
	}
	if binary, known := bare.BinaryHint("x"); binary || known {
		t.Fatalf("a bare repository without attributes reported %v, %v", binary, known)
	}
}

func TestRepoDiffOptionsReportBrokenSettings(t *testing.T) {
	unknown := newTestRepo(t)
	unknown.appendConfig("[diff]\n\talgorithm = fancy\n")
	if _, err := repoDiffOptions(unknown.reopen(), diff.Options{}); !errors.Is(err, diff.ErrUnknownAlgorithm) {
		t.Errorf("an unknown algorithm returned %v", err)
	}
	attributesFile := newTestRepo(t)
	attributesFile.appendConfig("[core]\n\tattributesfile = ~otheruser/attrs\n")
	if _, err := repoDiffOptions(attributesFile.reopen(), diff.Options{}); err == nil {
		t.Error("an unusable core.attributesfile was accepted")
	}
}

func TestDiffReadersFailOnAnUnknownAlgorithm(t *testing.T) {
	tr := newTestRepo(t)
	tr.comparableFork()
	tr.remove("f")
	tr.writeFile("moved", tenLines("f"))
	tr.commitFiles("move", map[string]string{"f": "", "moved": changeLine(tenLines("f"), 0, "OURS")})
	tr.appendConfig("[diff]\n\talgorithm = fancy\n")
	tr.repo = tr.reopen()

	if _, err := Compare(t.Context(), tr.repo, "main", "feature", CompareOptions{}); !errors.Is(err, diff.ErrUnknownAlgorithm) {
		t.Errorf("Compare returned %v", err)
	}
	if _, err := Details(t.Context(), tr.repo, "HEAD", DetailsOptions{}); !errors.Is(err, diff.ErrUnknownAlgorithm) {
		t.Errorf("Details returned %v", err)
	}
	if _, err := FileHistory(t.Context(), tr.repo, "HEAD", "moved", HistoryOptions{Follow: true}); !errors.Is(err, diff.ErrUnknownAlgorithm) {
		t.Errorf("FileHistory returned %v", err)
	}
}
