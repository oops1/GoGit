package changes

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/diffview"
)

func setupTruncationString(t *testing.T) {
	t.Helper()
	widget.RegisterString("en", "Diff.Truncated", "Diff truncated, showing a partial result")
	widget.SetLanguage("en")
	t.Cleanup(widget.ClearStrings)
}

func TestFromFileMapsContextAddedAndRemovedLinesWithLineNumbers(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt",
		NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 2,
			Lines: []diff.Line{
				{Kind: diff.KindContext, Text: "keep"},
				{Kind: diff.KindDel, Text: "old"},
				{Kind: diff.KindAdd, Text: "new"},
			},
		}},
	}

	doc := FromFile(f)

	if len(doc.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(doc.Hunks))
	}
	lines := doc.Hunks[0].Lines
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[0].Kind != diffview.Context || lines[0].OldNo != 1 || lines[0].NewNo != 1 || lines[0].Text != "keep" {
		t.Fatalf("context line = %+v", lines[0])
	}
	if lines[1].Kind != diffview.Removed || lines[1].OldNo != 2 || lines[1].NewNo != 0 || lines[1].Text != "old" {
		t.Fatalf("removed line = %+v", lines[1])
	}
	if lines[2].Kind != diffview.Added || lines[2].NewNo != 2 || lines[2].OldNo != 0 || lines[2].Text != "new" {
		t.Fatalf("added line = %+v", lines[2])
	}
}

func TestFromFileBuildsHunkBannerFromHunkRanges(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 3, OldLines: 2, NewStart: 3, NewLines: 1, Header: "func Foo()",
			Lines: []diff.Line{
				{Kind: diff.KindDel, Text: "one"},
				{Kind: diff.KindContext, Text: "two"},
			},
		}},
	}

	doc := FromFile(f)

	want := "@@ -3,2 +3 @@ func Foo()"
	if got := doc.Hunks[0].Header; got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestFromFileBuildsHunkBannerWithoutTrailingCommaForSingleLineRanges(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 5, OldLines: 1, NewStart: 5, NewLines: 1,
			Lines: []diff.Line{{Kind: diff.KindContext, Text: "line"}},
		}},
	}

	doc := FromFile(f)

	if got := doc.Hunks[0].Header; got != "@@ -5 +5 @@" {
		t.Fatalf("header = %q, want %q", got, "@@ -5 +5 @@")
	}
}

func TestFromFileBuildsHunkBannerWithDecrementedStartForEmptyRange(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 5, OldLines: 0, NewStart: 5, NewLines: 1,
			Lines: []diff.Line{{Kind: diff.KindAdd, Text: "line"}},
		}},
	}

	doc := FromFile(f)

	if got := doc.Hunks[0].Header; got != "@@ -4,0 +5 @@" {
		t.Fatalf("header = %q, want %q", got, "@@ -4,0 +5 @@")
	}
}

func TestFromFileAppendsNoNewlineMarkerAfterTheAffectedLine(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
			Lines: []diff.Line{{Kind: diff.KindContext, Text: "eof", NoNewline: true}},
		}},
	}

	doc := FromFile(f)

	lines := doc.Hunks[0].Lines
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0].Text != "eof" {
		t.Fatalf("first line = %+v", lines[0])
	}
	if lines[1].Kind != diffview.NoNewline || lines[1].Text != "No newline at end of file" {
		t.Fatalf("marker line = %+v", lines[1])
	}
}

func TestFromFileMarksBinaryFilesWithoutHunksOrLines(t *testing.T) {
	f := diff.File{OldPath: "img.png", NewPath: "img.png", Binary: true, Hunks: []diff.Hunk{{
		Lines: []diff.Line{{Kind: diff.KindContext, Text: "should not appear"}},
	}}}

	doc := FromFile(f)

	if !doc.Binary {
		t.Fatal("Binary must be true")
	}
	if len(doc.Hunks) != 0 {
		t.Fatalf("hunks = %d, want 0 for a binary file", len(doc.Hunks))
	}
	if doc.OldName != "img.png" || doc.NewName != "img.png" {
		t.Fatalf("names = %q/%q", doc.OldName, doc.NewName)
	}
}

func TestFromFileHighlightsInlineChangesForRemovedAddedLinePairs(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
			Lines: []diff.Line{
				{Kind: diff.KindDel, Text: "hello world"},
				{Kind: diff.KindAdd, Text: "hello there"},
			},
		}},
	}

	doc := FromFile(f)

	lines := doc.Hunks[0].Lines
	removed, added := lines[0], lines[1]
	if len(removed.Spans) != 1 || removed.Spans[0] != (diffview.Span{Start: 6, End: 11}) {
		t.Fatalf("removed spans = %+v, want span over %q", removed.Spans, "world")
	}
	if len(added.Spans) != 1 || added.Spans[0] != (diffview.Span{Start: 6, End: 11}) {
		t.Fatalf("added spans = %+v, want span over %q", added.Spans, "there")
	}
}

func TestFromFileLeavesSpansEmptyWhenRemovedAndAddedLinesAreIdentical(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			Lines: []diff.Line{
				{Kind: diff.KindDel, Text: "same"},
				{Kind: diff.KindAdd, Text: "same"},
			},
		}},
	}

	doc := FromFile(f)

	lines := doc.Hunks[0].Lines
	if lines[0].Spans != nil || lines[1].Spans != nil {
		t.Fatalf("spans = %+v / %+v, want none for identical lines", lines[0].Spans, lines[1].Spans)
	}
}

func TestFromFileLeavesUnpairedRemovedAndAddedLinesWithoutSpans(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			Lines: []diff.Line{
				{Kind: diff.KindDel, Text: "removed one"},
				{Kind: diff.KindDel, Text: "removed two"},
				{Kind: diff.KindAdd, Text: "added one"},
			},
		}},
	}

	doc := FromFile(f)

	lines := doc.Hunks[0].Lines
	if lines[0].Spans == nil {
		t.Fatal("first removed line must be paired with the added line")
	}
	if lines[1].Spans != nil {
		t.Fatalf("second removed line has no counterpart, spans = %+v", lines[1].Spans)
	}
	if lines[2].Spans == nil {
		t.Fatal("added line must be paired with the first removed line")
	}
}

func manyContextLines(n int) []diff.Line {
	lines := make([]diff.Line, n)
	for i := range lines {
		lines[i] = diff.Line{Kind: diff.KindContext, Text: "line"}
	}
	return lines
}

func TestFromFileDoesNotTruncateWhenWithinTheLineBudget(t *testing.T) {
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{OldStart: 1, OldLines: 3, NewStart: 1, NewLines: 3, Lines: manyContextLines(3)}},
	}

	doc := FromFile(f)

	if len(doc.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(doc.Hunks))
	}
	if len(doc.Hunks[0].Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(doc.Hunks[0].Lines))
	}
}

func TestFromFileTruncatesMidHunkWhenExceedingTheLineBudgetAndAppendsAMarkerHunk(t *testing.T) {
	setupTruncationString(t)
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{{
			OldStart: 1, OldLines: MaxLinesPerFile + 10, NewStart: 1, NewLines: MaxLinesPerFile + 10,
			Lines: manyContextLines(MaxLinesPerFile + 10),
		}},
	}

	doc := FromFile(f)

	if len(doc.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2 (truncated content hunk + marker)", len(doc.Hunks))
	}
	if len(doc.Hunks[0].Lines) != MaxLinesPerFile {
		t.Fatalf("lines in first hunk = %d, want %d", len(doc.Hunks[0].Lines), MaxLinesPerFile)
	}
	marker := doc.Hunks[1]
	if len(marker.Lines) != 0 || marker.Header != i18n.T("Diff.Truncated") {
		t.Fatalf("marker hunk = %+v", marker)
	}
}

func TestFromFileTruncatesBetweenHunksWhenTheFirstHunkExhaustsTheBudget(t *testing.T) {
	setupTruncationString(t)
	f := diff.File{
		OldPath: "a.txt", NewPath: "a.txt",
		Hunks: []diff.Hunk{
			{OldStart: 1, OldLines: MaxLinesPerFile, NewStart: 1, NewLines: MaxLinesPerFile, Lines: manyContextLines(MaxLinesPerFile)},
			{OldStart: 10000, OldLines: 1, NewStart: 10000, NewLines: 1, Lines: manyContextLines(1)},
		},
	}

	doc := FromFile(f)

	if len(doc.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2 (full first hunk + marker)", len(doc.Hunks))
	}
	if len(doc.Hunks[0].Lines) != MaxLinesPerFile {
		t.Fatalf("lines in first hunk = %d, want %d", len(doc.Hunks[0].Lines), MaxLinesPerFile)
	}
	if doc.Hunks[1].Header != i18n.T("Diff.Truncated") {
		t.Fatalf("second hunk = %+v, want the truncation marker", doc.Hunks[1])
	}
}
