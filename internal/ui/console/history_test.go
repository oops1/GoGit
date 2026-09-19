package console

import (
	"slices"
	"strconv"
	"testing"
)

func TestHistoryWalksBackAndForward(t *testing.T) {
	var h History
	h.Add("status")
	h.Add("log --oneline")

	if text, ok := h.Previous(); !ok || text != "log --oneline" {
		t.Fatalf("previous = %q, ok = %v", text, ok)
	}
	if text, ok := h.Previous(); !ok || text != "status" {
		t.Fatalf("previous = %q, ok = %v", text, ok)
	}
	if _, ok := h.Previous(); ok {
		t.Fatal("the first entry has nothing before it")
	}
	if text, ok := h.Next(); !ok || text != "log --oneline" {
		t.Fatalf("next = %q, ok = %v", text, ok)
	}
	if text, ok := h.Next(); !ok || text != "" {
		t.Fatalf("next = %q, ok = %v", text, ok)
	}
	if _, ok := h.Next(); ok {
		t.Fatal("the fresh line has nothing after it")
	}
}

func TestHistoryIgnoresBlankAndRepeatedLines(t *testing.T) {
	var h History
	h.Add("status")
	h.Add("status")
	h.Add("   ")

	if got := h.Items(); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("items = %#v", got)
	}
	if text, ok := h.Previous(); !ok || text != "status" {
		t.Fatalf("previous = %q, ok = %v", text, ok)
	}
}

func TestHistoryTrimsItselfToTheLatestLines(t *testing.T) {
	var h History
	for i := range maxHistory + 10 {
		h.Add("command " + strconv.Itoa(i))
	}

	items := h.Items()
	if len(items) != maxHistory || items[0] != "command 10" {
		t.Fatalf("items = %d, first = %q", len(items), items[0])
	}
}

func TestHistoryStartsAtTheFreshLine(t *testing.T) {
	var h History
	if _, ok := h.Previous(); ok {
		t.Fatal("an empty history has nothing before it")
	}
	if _, ok := h.Next(); ok {
		t.Fatal("an empty history has nothing after it")
	}
}
