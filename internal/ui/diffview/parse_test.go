package diffview

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseFixture(t *testing.T, name string) Document {
	t.Helper()
	doc, err := ParseUnified(readFixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestParseUnifiedReadsSimpleModification(t *testing.T) {
	doc := parseFixture(t, "simple.diff")
	if doc.OldName != "main.go" || doc.NewName != "main.go" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
	if doc.Binary {
		t.Fatal("text diff reported as binary")
	}
	if len(doc.Hunks) != 1 {
		t.Fatalf("hunks = %d", len(doc.Hunks))
	}
	hunk := doc.Hunks[0]
	if hunk.Header != "@@ -3,5 +3,5 @@ package main" {
		t.Fatalf("header = %q", hunk.Header)
	}
	want := []Line{
		{Kind: Context, OldNo: 3, NewNo: 3, Text: `import "fmt"`},
		{Kind: Context, OldNo: 4, NewNo: 4, Text: ""},
		{Kind: Context, OldNo: 5, NewNo: 5, Text: "func main() {"},
		{Kind: Removed, OldNo: 6, Text: "\tfmt.Println(\"hello\")"},
		{Kind: Added, NewNo: 6, Text: "\tfmt.Println(\"hello, world\")"},
		{Kind: Context, OldNo: 7, NewNo: 7, Text: "}"},
	}
	if len(hunk.Lines) != len(want) {
		t.Fatalf("lines = %d, want %d", len(hunk.Lines), len(want))
	}
	for i, line := range want {
		got := hunk.Lines[i]
		if got.Kind != line.Kind || got.OldNo != line.OldNo || got.NewNo != line.NewNo || got.Text != line.Text {
			t.Fatalf("line %d = %+v, want %+v", i, got, line)
		}
	}
	assertSpans(t, "removed", hunk.Lines[3].Spans, nil)
	assertSpans(t, "added", hunk.Lines[4].Spans, []Span{{Start: 19, End: 26}})
}

func TestParseUnifiedReadsEveryHunk(t *testing.T) {
	doc := parseFixture(t, "multihunk.diff")
	if len(doc.Hunks) != 2 {
		t.Fatalf("hunks = %d", len(doc.Hunks))
	}
	if doc.Hunks[1].Header != "@@ -15,6 +15,6 @@ fourteen" {
		t.Fatalf("second header = %q", doc.Hunks[1].Header)
	}
	first := doc.Hunks[1].Lines[0]
	if first.OldNo != 15 || first.NewNo != 15 {
		t.Fatalf("first line of second hunk = %+v", first)
	}
	removed := doc.Hunks[1].Lines[3]
	if removed.Kind != Removed || removed.OldNo != 18 || removed.Text != "eighteen" {
		t.Fatalf("removed = %+v", removed)
	}
}

func TestParseUnifiedReadsNewFile(t *testing.T) {
	doc := parseFixture(t, "newfile.diff")
	if doc.OldName != "" || doc.NewName != "added.txt" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
	for i, line := range doc.Hunks[0].Lines {
		if line.Kind != Added || line.OldNo != 0 || line.NewNo != i+1 {
			t.Fatalf("line %d = %+v", i, line)
		}
	}
}

func TestParseUnifiedReadsDeletedFile(t *testing.T) {
	doc := parseFixture(t, "deleted.diff")
	if doc.OldName != "renamed.txt" || doc.NewName != "" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
	line := doc.Hunks[0].Lines[0]
	if line.Kind != Removed || line.OldNo != 1 || line.NewNo != 0 || line.Text != "keep me" {
		t.Fatalf("line = %+v", line)
	}
}

func TestParseUnifiedMarksMissingTrailingNewline(t *testing.T) {
	doc := parseFixture(t, "nonewline.diff")
	kinds := make([]Kind, 0, len(doc.Hunks[0].Lines))
	for _, line := range doc.Hunks[0].Lines {
		kinds = append(kinds, line.Kind)
	}
	want := []Kind{Context, Removed, NoNewline, Added, NoNewline}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	if doc.Hunks[0].Lines[2].Text != "No newline at end of file" {
		t.Fatalf("marker text = %q", doc.Hunks[0].Lines[2].Text)
	}
}

func TestParseUnifiedDetectsBinaryFile(t *testing.T) {
	doc := parseFixture(t, "binary.diff")
	if !doc.Binary {
		t.Fatal("binary diff not detected")
	}
	if len(doc.Hunks) != 0 {
		t.Fatalf("hunks = %d", len(doc.Hunks))
	}
	if doc.OldName != "blob.bin" || doc.NewName != "blob.bin" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
}

func TestParseUnifiedReadsRenameWithoutHunks(t *testing.T) {
	doc := parseFixture(t, "rename.diff")
	if doc.OldName != "renamed.txt" || doc.NewName != "moved.txt" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
	if !doc.IsEmpty() {
		t.Fatal("rename without hunks must be empty")
	}
}

func TestParseUnifiedDetectsGitBinaryPatch(t *testing.T) {
	doc, err := ParseUnified([]byte("diff --git a/x b/x\nGIT binary patch\nliteral 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Binary {
		t.Fatal("GIT binary patch not detected")
	}
}

func TestParseUnifiedAcceptsPlainDiffHeadersWithTimestamps(t *testing.T) {
	input := "--- old.txt\t2024-01-01 00:00:00\n+++ new.txt\t2024-01-02 00:00:00\n@@ -1 +1 @@\n-a\n+b\n"
	doc, err := ParseUnified([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if doc.OldName != "old.txt" || doc.NewName != "new.txt" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
	if doc.Hunks[0].Lines[0].OldNo != 1 || doc.Hunks[0].Lines[1].NewNo != 1 {
		t.Fatalf("numbers = %+v", doc.Hunks[0].Lines)
	}
}

func TestParseUnifiedReadsNamesFromGitHeaderWithoutDestinationPrefix(t *testing.T) {
	doc, err := ParseUnified([]byte("diff --git a/only.txt\n"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.OldName != "only.txt" || doc.NewName != "only.txt" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
}

func TestParseUnifiedReturnsEmptyDocumentForEmptyInput(t *testing.T) {
	doc, err := ParseUnified(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.IsEmpty() {
		t.Fatal("empty input must produce an empty document")
	}
}

func TestParseUnifiedSkipsContextBeforeFirstHunk(t *testing.T) {
	doc, err := ParseUnified([]byte("diff --git a/x b/x\n context\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !doc.IsEmpty() {
		t.Fatalf("document = %+v", doc)
	}
}

func TestParseUnifiedStopsHunkOnUnrelatedTrailer(t *testing.T) {
	input := "@@ -1,1 +1,1 @@\n-a\n+b\n1 file changed, 1 insertion(+), 1 deletion(-)\n"
	doc, err := ParseUnified([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Hunks[0].Lines) != 2 {
		t.Fatalf("lines = %+v", doc.Hunks[0].Lines)
	}
}

func TestParseUnifiedFailsOnSecondFile(t *testing.T) {
	input := "diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\ndiff --git a/y b/y\n@@ -1 +1 @@\n-c\n+d\n"
	if _, err := ParseUnified([]byte(input)); !errors.Is(err, ErrMultipleFiles) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseUnifiedFailsOnMalformedHunkHeader(t *testing.T) {
	cases := map[string]string{
		"no leading marker":  "@@-1,1 +1,1 @@\n",
		"no trailing marker": "@@ -1,1 +1,1\n",
		"single range":       "@@ -1,1 @@\n",
		"old range sign":     "@@ +1,1 +1,1 @@\n",
		"new range sign":     "@@ -1,1 -1,1 @@\n",
		"old range digits":   "@@ -x,1 +1,1 @@\n",
		"new range digits":   "@@ -1,1 +x,1 @@\n",
		"negative start":     "@@ --1,1 +1,1 @@\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseUnified([]byte(input)); !errors.Is(err, ErrHunkHeader) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestParseUnifiedFailsOnContentBeforeHunkHeader(t *testing.T) {
	cases := map[string]string{
		"added":      "diff --git a/x b/x\n+a\n",
		"removed":    "diff --git a/x b/x\n-a\n",
		"no newline": "diff --git a/x b/x\n\\ No newline at end of file\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseUnified([]byte(input)); !errors.Is(err, ErrLineOutsideHunk) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestParseUnifiedKeepsHeadersInsideHunkAsContent(t *testing.T) {
	input := "@@ -1,2 +1,2 @@\n--- a\n+++ b\n"
	doc, err := ParseUnified([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	lines := doc.Hunks[0].Lines
	if len(lines) != 2 || lines[0].Kind != Removed || lines[0].Text != "-- a" {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[1].Kind != Added || lines[1].Text != "++ b" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestParseUnifiedSpans(t *testing.T) {
	cases := []struct {
		name    string
		old     string
		updated string
		oldWant []Span
		newWant []Span
	}{
		{name: "common prefix and suffix", old: "abcXdef", updated: "abcYdef",
			oldWant: []Span{{Start: 3, End: 4}}, newWant: []Span{{Start: 3, End: 4}}},
		{name: "insertion keeps old side clean", old: "ad", updated: "abcd",
			oldWant: nil, newWant: []Span{{Start: 1, End: 3}}},
		{name: "deletion keeps new side clean", old: "abcd", updated: "ad",
			oldWant: []Span{{Start: 1, End: 3}}, newWant: nil},
		{name: "nothing in common", old: "abc", updated: "xyz", oldWant: nil, newWant: nil},
		{name: "identical texts", old: "same", updated: "same", oldWant: nil, newWant: nil},
		{name: "empty texts", old: "", updated: "", oldWant: nil, newWant: nil},
		{name: "unicode", old: "привет мир", updated: "привет свет",
			oldWant: []Span{{Start: 7, End: 10}}, newWant: []Span{{Start: 7, End: 11}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := ParseUnified([]byte("@@ -1 +1 @@\n-" + c.old + "\n+" + c.updated + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			assertSpans(t, "old", doc.Hunks[0].Lines[0].Spans, c.oldWant)
			assertSpans(t, "new", doc.Hunks[0].Lines[1].Spans, c.newWant)
		})
	}
}

func assertSpans(t *testing.T, side string, got, want []Span) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s spans = %+v, want %+v", side, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s spans = %+v, want %+v", side, got, want)
		}
	}
}

func TestParseUnifiedLeavesUnpairedRemovalsWithoutSpans(t *testing.T) {
	doc, err := ParseUnified([]byte("@@ -1,2 +1,1 @@\n-abc\n-abd\n+abe\n"))
	if err != nil {
		t.Fatal(err)
	}
	lines := doc.Hunks[0].Lines
	if len(lines[0].Spans) != 1 {
		t.Fatalf("first removal spans = %+v", lines[0].Spans)
	}
	if lines[1].Spans != nil {
		t.Fatalf("second removal spans = %+v", lines[1].Spans)
	}
}

func TestParseUnifiedSkipsAdditionsWithoutRemovals(t *testing.T) {
	doc, err := ParseUnified([]byte("@@ -1,1 +1,2 @@\n a\n+b\n"))
	if err != nil {
		t.Fatal(err)
	}
	lines := doc.Hunks[0].Lines
	if len(lines) != 2 || lines[1].Kind != Added || lines[1].Spans != nil {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestParseUnifiedAcceptsInputWithoutTrailingNewline(t *testing.T) {
	doc, err := ParseUnified([]byte("@@ -1 +1 @@\n-a\n+b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Hunks[0].Lines) != 2 {
		t.Fatalf("lines = %+v", doc.Hunks[0].Lines)
	}
}

func TestParseUnifiedReadsZeroRangeOfNewFile(t *testing.T) {
	doc, err := ParseUnified([]byte("--- /dev/null\n+++ b/x\n@@ -0,0 +1 @@\n+a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.OldName != "" || doc.NewName != "x" {
		t.Fatalf("names = %q %q", doc.OldName, doc.NewName)
	}
}

func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		Context:    "context",
		Added:      "added",
		Removed:    "removed",
		HunkHeader: "hunk",
		NoNewline:  "nonewline",
		Kind(99):   "unknown",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", int(kind), got, want)
		}
	}
}

func TestDocumentCloneIsIndependent(t *testing.T) {
	doc := parseFixture(t, "simple.diff")
	clone := doc.Clone()
	clone.Hunks[0].Lines[0].Text = "changed"
	clone.Hunks[0].Lines[4].Spans[0] = Span{Start: 0, End: 1}
	if doc.Hunks[0].Lines[0].Text == "changed" {
		t.Fatal("clone shares line storage")
	}
	if doc.Hunks[0].Lines[4].Spans[0] == (Span{Start: 0, End: 1}) {
		t.Fatal("clone shares span storage")
	}
}

func TestDocumentIsEmpty(t *testing.T) {
	if !(Document{}).IsEmpty() {
		t.Fatal("zero document must be empty")
	}
	if (Document{Binary: true}).IsEmpty() {
		t.Fatal("binary document must not be empty")
	}
	if (Document{Hunks: []Hunk{{Header: "@@"}}}).IsEmpty() {
		t.Fatal("document with a hunk header must not be empty")
	}
	if !(Document{Hunks: []Hunk{{}}}).IsEmpty() {
		t.Fatal("document with an empty hunk must be empty")
	}
}
