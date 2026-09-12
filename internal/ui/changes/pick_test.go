package changes

import (
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/patch"
	"github.com/oops1/gogit/internal/ui/diffview"
)

func fileOf(old, new string) diff.File {
	return diff.File{OldPath: "f", NewPath: "f", Hunks: diff.Blobs([]byte(old), []byte(new), diff.Defaults())}
}

func refOf(t *testing.T, doc diffview.Document, kind diffview.Kind, text string) diffview.LineRef {
	t.Helper()
	for hunk, h := range doc.Hunks {
		for line, l := range h.Lines {
			if l.Kind == kind && l.Text == text {
				return diffview.LineRef{Hunk: hunk, Line: line}
			}
		}
	}
	t.Fatalf("no %v line %q", kind, text)
	return diffview.LineRef{}
}

func TestTheLinesPickedOnScreenAreTheLinesStaged(t *testing.T) {
	old, new := "a\nb\nc\n", "a\nB\nc\nd\n"
	file := fileOf(old, new)
	doc := FromFile(file)

	selected, err := patch.Select(file.Hunks, Picked(file, []diffview.LineRef{refOf(t, doc, diffview.Added, "d")}))

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	got, err := diff.Apply([]byte(old), selected)
	if err != nil || string(got) != "a\nb\nc\nd\n" {
		t.Fatalf("staged = %q, %v", got, err)
	}
}

func TestAMissingNewlineRowShiftsTheLinesAfterIt(t *testing.T) {
	file := fileOf("a", "b\n")
	doc := FromFile(file)
	ref := refOf(t, doc, diffview.Added, "b")

	pick := Picked(file, []diffview.LineRef{ref})

	if ref.Line != 2 || !pick(0, 1) || pick(0, 0) {
		t.Fatalf("ref = %+v, picks the added line = %v, the removed one = %v", ref, pick(0, 1), pick(0, 0))
	}
}

func TestRefsOutsideTheDiffPickNothing(t *testing.T) {
	file := fileOf("a", "b\n")

	pick := Picked(file, []diffview.LineRef{{Hunk: 5}, {Hunk: -1}, {Hunk: 0, Line: 1}, {Hunk: 0, Line: 99}})

	for line := range 3 {
		if pick(0, line) {
			t.Fatalf("line %d was picked", line)
		}
	}
}

func TestPickedHunksTakeEveryLineOfThem(t *testing.T) {
	pick := PickedHunks([]int{1})

	if !pick(1, 7) || pick(0, 0) {
		t.Fatalf("hunk 1 = %v, hunk 0 = %v", pick(1, 7), pick(0, 0))
	}
}
