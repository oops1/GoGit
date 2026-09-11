package journal

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func commitWithMessage(author, message string) *revision.Commit {
	sig := object.Signature{Name: author, Email: "a@example.com", When: time.Unix(1700000000, 0)}
	return &revision.Commit{
		ID:     hash.SumSHA1(object.TypeCommit.String(), []byte(message)),
		Commit: &object.Commit{Author: sig, Committer: sig, Message: message},
	}
}

func TestARowNamesThePeopleTheCommitCreditsButNotItsAuthor(t *testing.T) {
	commit := commitWithMessage("Ann", "work\n\nCo-Authored-By: Claude Opus 5 <noreply@anthropic.com>\nSigned-off-by: Ann <ann@example.com>\nHelped-by: Bob\n")

	row := newRow(commit, nil, nil)

	if len(row.Credited) != 2 || row.Credited[0] != "Claude Opus 5" || row.Credited[1] != "Bob" {
		t.Fatalf("credited = %q, want the others in the order of the message", row.Credited)
	}
	if row.AuthorLine != "Ann, Claude Opus 5, Bob" {
		t.Fatalf("author line = %q, want the author then the others, separated by commas", row.AuthorLine)
	}
}

func TestTheAuthorLineIsJustTheAuthorWhenNobodyElseIsCredited(t *testing.T) {
	if got := authorLine("Ann", nil); got != "Ann" {
		t.Fatalf("line = %q, want the author alone", got)
	}
	if got := authorLine("", []string{"Bob"}); got != "Bob" {
		t.Fatalf("line = %q, want the credited person alone", got)
	}
}

func TestCreditedPeopleAreShownAsSquaresAfterTheAuthor(t *testing.T) {
	v := NewView()
	v.Restyle(widget.Win11LightTheme())
	dc := &recordingBadgeCtx{}
	row := Row{Author: "Ann", Credited: []string{"Claude Opus", "Bob Smith"}}

	v.drawAuthorBadgeCell(datagrid.CellDrawContext{
		Rect: image.Rect(0, 0, 200, 20), Item: row, DrawCtx: dc, FontSize: 9,
	})

	if len(dc.images) != 1 {
		t.Fatalf("images = %d, want the author's own badge", len(dc.images))
	}
	if len(dc.badges) != 2 {
		t.Fatalf("squares = %d, want one per credited person", len(dc.badges))
	}
	for _, square := range dc.badges {
		if square.color != v.credit.fill {
			t.Fatalf("square colour = %v, want the colour of credited people", square.color)
		}
		if square.x <= dc.images[0].x || square.w != square.h {
			t.Fatalf("square = %+v, want a square after the author", square)
		}
	}
	for _, author := range badgePalette {
		if v.credit.fill == author {
			t.Fatalf("credited fill %v is one of the author colours: a credited person must not look like an author", author)
		}
	}
	labels := []string{}
	for _, text := range dc.texts {
		labels = append(labels, text.text)
	}
	if len(labels) != 3 || labels[1] != Initials("Claude Opus") || labels[2] != Initials("Bob Smith") {
		t.Fatalf("labels = %q, want the author's initials and then the credited ones", labels)
	}
}

func TestCreditedSquaresStillShowUnderARepeatedAuthor(t *testing.T) {
	v, _ := bound(t)
	v.Append([]Row{{Author: "Ann"}, {Author: "Ann", Credited: []string{"Bob"}}})
	dc := &recordingBadgeCtx{}

	v.drawAuthorBadgeCell(datagrid.CellDrawContext{
		Rect: image.Rect(0, 20, 200, 40), Item: Row{Author: "Ann", Credited: []string{"Bob"}}, DrawCtx: dc, FontSize: 9, RowIndex: 1,
	})

	if len(dc.images) != 0 || len(dc.badges) != 1 {
		t.Fatalf("images = %d, squares = %d, want the author left out and the credited person shown", len(dc.images), len(dc.badges))
	}
}

func TestSquaresThatDoNotFitTheCellAreLeftOut(t *testing.T) {
	v := NewView()
	dc := &recordingBadgeCtx{}

	v.drawAuthorBadgeCell(datagrid.CellDrawContext{
		Rect: image.Rect(0, 0, 50, 20), Item: Row{Author: "Ann", Credited: []string{"A B", "C D", "E F"}}, DrawCtx: dc, FontSize: 9,
	})

	if len(dc.badges) != 1 {
		t.Fatalf("squares = %d, want only the one that fits", len(dc.badges))
	}
}

func TestALongListOfCreditedPeopleEndsWithTheirCount(t *testing.T) {
	few := creditedLabels([]string{"A B", "C D", "E F", "G H"})
	many := creditedLabels([]string{"A B", "C D", "E F", "G H", "I J", "K L"})

	if len(few) != 4 || few[3] != Initials("G H") {
		t.Fatalf("labels = %q, want every one of four people", few)
	}
	if len(many) != creditedBadgesShown || many[3] != creditedMorePrefix+"3" {
		t.Fatalf("labels = %q, want three people and a square for the other three", many)
	}
}

func TestTheAuthorColumnWidensForTheMostCreditedRowInView(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	v.SetFullAuthorName(false)
	plain := grid.Grid.Columns()[authorColumnIndex].Width().Value

	v.Append([]Row{{Author: "Ann", Credited: []string{"Bob", "Claude"}}})

	wide := grid.Grid.Columns()[authorColumnIndex].Width().Value
	if int(wide) != authorColumnWidth(grid.Grid.RowHeight, 2) || wide <= plain {
		t.Fatalf("width = %v, want room for two more squares next to %v", wide, plain)
	}

	v.Append([]Row{{Author: "Ann", Credited: []string{"a", "b", "c", "d", "e", "f"}}})
	if got := grid.Grid.Columns()[authorColumnIndex].Width().Value; int(got) != authorColumnWidth(grid.Grid.RowHeight, creditedBadgesShown) {
		t.Fatalf("width = %v, want it capped at %d squares", got, creditedBadgesShown)
	}
}

func TestTheFullNameColumnKeepsItsWidthWhateverTheCredits(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	v.SetFullAuthorName(true)

	v.Append([]Row{{Author: "Ann", Credited: []string{"Bob"}}})

	if got := grid.Grid.Columns()[authorColumnIndex].Width().Value; got != authorBadgeColumnWide {
		t.Fatalf("width = %v, want the full-name column left at %d", got, authorBadgeColumnWide)
	}
}

func TestTheColourOfCreditedPeopleFollowsTheTheme(t *testing.T) {
	v := NewView()
	v.Restyle(widget.Win11DarkTheme())
	dark := v.credit

	v.Restyle(widget.Win11LightTheme())

	if v.credit == dark || v.credit.fill == (color.RGBA{}) {
		t.Fatalf("credit = %+v, want it recoloured for the light theme", v.credit)
	}
}
