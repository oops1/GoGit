package gitdiff

import (
	"image"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func shownView(t *testing.T) *View {
	t.Helper()
	v := New()
	v.SetBounds(image.Rect(0, 0, 800, 400))
	v.Show(Side{Title: "a.txt", Text: "one\ntwo\nthree\n"}, Side{Title: "a.txt", Text: "one\nTWO\nthree\n"})
	return v
}

func rightPress(v *View, x, y int) {
	v.OnMouseButton(widget.MouseEvent{Button: widget.MouseRight, Pressed: true, X: x, Y: y})
}

func pointInText(t *testing.T, v *View, side widget.DiffSide) (int, int) {
	t.Helper()
	b := v.Bounds()
	half := b.Dx() / 2
	from, to := b.Min.X, b.Min.X+half
	if side == widget.DiffRight {
		from, to = b.Min.X+half, b.Max.X
	}
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := from; x < to; x += 4 {
			rightPress(v, x, y)
			if v.ActiveSide() == side && v.DiffView.ContextMenuAt(x, y) != nil {
				return x, y
			}
		}
	}
	t.Fatal("no text area found")
	return 0, 0
}

func texts(items []widget.MenuItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.Separator {
			out = append(out, "-")
			continue
		}
		out = append(out, item.Text)
	}
	return out
}

func TestANewViewComparesReadOnlyWithoutHeadersAndWithUnchangedLinesAndSpacesHidden(t *testing.T) {
	v := New()

	if !v.IsReadOnly(widget.DiffLeft) || !v.IsReadOnly(widget.DiffRight) || !v.HideUnchanged() {
		t.Fatalf("read only = %v/%v, hide unchanged = %v", v.IsReadOnly(widget.DiffLeft), v.IsReadOnly(widget.DiffRight), v.HideUnchanged())
	}
	if v.ShowHeaders() || v.ShowReadOnlyMark() {
		t.Fatalf("headers = %v, read-only mark = %v", v.ShowHeaders(), v.ShowReadOnlyMark())
	}
}

func TestShowPutsBothSidesAndClearEmptiesThem(t *testing.T) {
	v := shownView(t)

	if v.Text(widget.DiffLeft) != "one\ntwo\nthree\n" || v.Text(widget.DiffRight) != "one\nTWO\nthree\n" || v.ChangeCount() != 1 {
		t.Fatalf("left = %q, right = %q, changes = %d", v.Text(widget.DiffLeft), v.Text(widget.DiffRight), v.ChangeCount())
	}

	v.Clear()

	if v.Text(widget.DiffLeft) != "" || v.Text(widget.DiffRight) != "" || v.ChangeCount() != 0 {
		t.Fatalf("after clear: left = %q, right = %q", v.Text(widget.DiffLeft), v.Text(widget.DiffRight))
	}
}

func TestTheMarkupTagBuildsTheView(t *testing.T) {
	Register()

	_, named, err := widget.LoadUIFromXAML([]byte(`<Window><GitDiffView x:Name="diff"/></Window>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := named["diff"].(*View); !ok {
		t.Fatalf("diff = %T, want *gitdiff.View", named["diff"])
	}
}

func TestTheMenuAddsTheOwnerItemsForTheLineUnderTheCaret(t *testing.T) {
	v := shownView(t)
	x, y := pointInText(t, v, widget.DiffRight)
	var asked []Spot
	v.OnMenu = func(spot Spot) []widget.MenuItem {
		asked = append(asked, spot)
		return []widget.MenuItem{{Text: "stage"}}
	}

	menu := v.ContextMenuAt(x, y)

	items := menu.Items()
	if len(asked) != 1 || asked[0].Side != widget.DiffRight || asked[0].From < 0 || asked[0].To != asked[0].From+1 {
		t.Fatalf("asked = %+v", asked)
	}
	if got := texts(items); len(got) < 3 || got[len(got)-1] != "stage" || got[len(got)-2] != "-" {
		t.Fatalf("menu = %v", got)
	}
	for _, item := range items {
		if item.Disabled {
			t.Fatalf("menu keeps the disabled item %q", item.Text)
		}
	}
}

func TestTheMenuOffersTheSelectedLines(t *testing.T) {
	v := shownView(t)
	x, y := pointInText(t, v, widget.DiffRight)
	v.SelectAll()
	var asked []Spot
	v.OnMenu = func(spot Spot) []widget.MenuItem {
		asked = append(asked, spot)
		return nil
	}

	rightPress(v, x, y)
	v.ContextMenuAt(x, y)

	if len(asked) != 1 || asked[0] != (Spot{Side: widget.DiffRight, From: 0, To: 3}) {
		t.Fatalf("asked = %+v", asked)
	}
}

func TestTheMenuStaysTheViewOwnWithoutOwnerItems(t *testing.T) {
	v := shownView(t)
	x, y := pointInText(t, v, widget.DiffLeft)
	want := texts(usable(v.DiffView.ContextMenuAt(x, y).Items()))

	withoutOwner := texts(v.ContextMenuAt(x, y).Items())
	v.OnMenu = func(Spot) []widget.MenuItem { return nil }
	emptyOwner := texts(v.ContextMenuAt(x, y).Items())

	if !slices.Equal(withoutOwner, want) || !slices.Equal(emptyOwner, want) {
		t.Fatalf("without owner = %v, empty owner = %v, want %v", withoutOwner, emptyOwner, want)
	}
}

func TestNoMenuOutsideTheText(t *testing.T) {
	v := shownView(t)
	v.OnMenu = func(Spot) []widget.MenuItem { t.Fatal("asked outside the text"); return nil }

	if menu := v.ContextMenuAt(-50, -50); menu != nil {
		t.Fatalf("menu = %v", texts(menu.Items()))
	}
}

func TestUsableDropsDisabledItemsAndLooseSeparators(t *testing.T) {
	sep := widget.MenuItem{Separator: true}
	items := []widget.MenuItem{sep, {Text: "undo", Disabled: true}, sep, {Text: "copy"}, sep, sep, {Text: "cut", Disabled: true}, sep, {Text: "next"}, sep}

	got := texts(usable(items))

	if !slices.Equal(got, []string{"copy", "-", "next"}) {
		t.Fatalf("usable = %v", got)
	}
	if got := usable(nil); len(got) != 0 {
		t.Fatalf("usable(nil) = %v", got)
	}
}
