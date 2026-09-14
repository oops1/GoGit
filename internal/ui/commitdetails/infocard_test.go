package commitdetails

import (
	"image"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func renderCard(t *testing.T, card *infoCard) {
	t.Helper()
	eng := engine.New(700, 420, 30)
	t.Cleanup(eng.Stop)
	card.SetBounds(image.Rect(0, 0, 700, 420))
	eng.SetRoot(card)
	_ = eng.RenderOnce()
}

func hitCenter(t *testing.T, card *infoCard, index int) image.Point {
	t.Helper()
	card.mu.Lock()
	defer card.mu.Unlock()
	if index >= len(card.hits) {
		t.Fatalf("hits = %d, want more than %d", len(card.hits), index)
	}
	r := card.hits[index].rect
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

func click(card *infoCard, pt image.Point) bool {
	return card.OnMouseButton(widget.MouseEvent{X: pt.X, Y: pt.Y, Button: widget.MouseLeft, Pressed: true})
}

func TestTheCardOffersToCopyTheHashAndToOpenTheParent(t *testing.T) {
	newTestView(t)
	card := newInfoCard()
	card.Show(details())
	var copied string
	var opened hash.ObjectID
	card.OnCopy = func(text string) { copied = text }
	card.OnParent = func(id hash.ObjectID) { opened = id }

	renderCard(t, card)

	if !click(card, hitCenter(t, card, 0)) || copied != id("head").String() {
		t.Fatalf("copied = %q, want the full hash", copied)
	}
	if !click(card, hitCenter(t, card, 1)) || opened != id("parent") {
		t.Fatalf("opened = %v, want the parent", opened)
	}
	if card.Cursor(hitCenter(t, card, 0).X, hitCenter(t, card, 0).Y) != widget.CursorHand {
		t.Fatal("the copy button does not show a hand")
	}
}

func TestTheCardIgnoresClicksOutsideItsButtons(t *testing.T) {
	newTestView(t)
	card := newInfoCard()
	card.Show(details())
	renderCard(t, card)

	if click(card, image.Pt(2, 2)) {
		t.Fatal("a click on empty space was taken")
	}
	if card.OnMouseButton(widget.MouseEvent{X: 2, Y: 2, Button: widget.MouseRight, Pressed: true}) {
		t.Fatal("a right click was taken")
	}
	if card.Cursor(2, 2) != widget.CursorArrow {
		t.Fatal("empty space shows a hand")
	}
}

func TestTheCardButtonsDoNothingWithoutHandlers(t *testing.T) {
	newTestView(t)
	card := newInfoCard()
	card.Show(details())
	renderCard(t, card)

	click(card, hitCenter(t, card, 0))
	click(card, hitCenter(t, card, 1))
}

func TestAMergeCommitWithTagsAndABodyGetsAButtonPerParent(t *testing.T) {
	newTestView(t)
	card := newInfoCard()
	model := details()
	model.Parents = []hash.ObjectID{id("first"), id("second")}
	card.Show(model)

	renderCard(t, card)

	card.mu.Lock()
	hits := len(card.hits)
	card.mu.Unlock()
	if hits != 3 {
		t.Fatalf("hits = %d, want the copy button and two parents", hits)
	}
}

func TestARootCommitWithoutAnAuthorStillDraws(t *testing.T) {
	newTestView(t)
	card := newInfoCard()
	card.Show(Details{Commit: id("root"), Message: "root", Committer: "bob"})

	renderCard(t, card)

	card.mu.Lock()
	hits := len(card.hits)
	card.mu.Unlock()
	if hits != 1 {
		t.Fatalf("hits = %d, want only the copy button", hits)
	}
}

func TestAnEmptyCardInvitesToPickACommit(t *testing.T) {
	newTestView(t)
	card := newInfoCard()

	renderCard(t, card)

	card.mu.Lock()
	hits := len(card.hits)
	card.mu.Unlock()
	if hits != 0 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestTheMessageSplitsIntoASubjectAndItsBody(t *testing.T) {
	subject, body := splitMessage("a subject\n\nfirst\nsecond\n")
	if subject != "a subject" || !slices.Equal(body, []string{"first", "second"}) {
		t.Fatalf("subject = %q, body = %q", subject, body)
	}
	subject, body = splitMessage("only a subject\n")
	if subject != "only a subject" || body != nil {
		t.Fatalf("subject = %q, body = %q", subject, body)
	}
}

func TestTheAvatarShowsTheFirstLetter(t *testing.T) {
	if initialOf("демо") != "Д" || initialOf("") != "" {
		t.Fatalf("initials = %q, %q", initialOf("демо"), initialOf(""))
	}
}

func TestTheViewHandsItsHandlersToTheCard(t *testing.T) {
	v := newTestView(t)
	copied := ""
	var opened hash.ObjectID
	v.SetCopyHandler(func(text string) { copied = text })
	v.SetParentHandler(func(parent hash.ObjectID) { opened = parent })

	v.info.OnCopy("x")
	v.info.OnParent(id("p"))

	if copied != "x" || opened != id("p") {
		t.Fatalf("copied = %q, opened = %v", copied, opened)
	}
}

func TestTheCardFollowsTheTheme(t *testing.T) {
	newTestView(t)
	card := newInfoCard()
	dark := widget.Win11DarkTheme()

	card.Restyle(dark)

	card.mu.Lock()
	defer card.mu.Unlock()
	if card.pal.text != dark.LabelText {
		t.Fatalf("text colour = %v, want the dark theme's", card.pal.text)
	}
}
