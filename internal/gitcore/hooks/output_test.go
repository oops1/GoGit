package hooks

import (
	"slices"
	"strings"
	"testing"
)

func withLimits(t *testing.T, line, stream, tail int) {
	t.Helper()
	previousLine, previousStream, previousTail := maxLineBytes, maxStreamBytes, tailLines
	maxLineBytes, maxStreamBytes, tailLines = line, stream, tail
	t.Cleanup(func() { maxLineBytes, maxStreamBytes, tailLines = previousLine, previousStream, previousTail })
}

func TestOutputSplitsLinesOnEveryLineEnding(t *testing.T) {
	events := &recorder{}
	out := newOutput("pre-commit", events.sink)

	for _, chunk := range []string{"one\ntw", "o\r\nthree\rfour\n\nfive"} {
		if n, err := out.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	tail, truncated := out.finish()

	want := []string{"one", "two", "three", "four", "", "five"}
	if got := events.lines(); !slices.Equal(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if !slices.Equal(tail, want) || truncated {
		t.Fatalf("tail = %q, truncated = %v", tail, truncated)
	}
}

func TestOutputBreaksOverlongLinesAndRepairsBrokenText(t *testing.T) {
	withLimits(t, 4, 1<<20, 20)
	events := &recorder{}
	out := newOutput("pre-commit", events.sink)

	_, _ = out.Write([]byte("abcdefg\xff\n"))
	out.finish()

	want := []string{"abcd", "efg�"}
	if got := events.lines(); !slices.Equal(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestOutputStopsStreamingAtTheCapButKeepsTheTail(t *testing.T) {
	withLimits(t, 4096, 10, 2)
	events := &recorder{}
	out := newOutput("pre-commit", events.sink)

	_, _ = out.Write([]byte("12345\n67890\nab\ncd\n"))
	tail, truncated := out.finish()

	if got := events.lines(); !slices.Equal(got, []string{"12345"}) {
		t.Fatalf("streamed = %q", got)
	}
	count := 0
	for _, e := range events.snapshot() {
		if e.Kind == EventTruncated {
			count++
		}
	}
	if count != 1 || !truncated {
		t.Fatalf("truncation events = %d, truncated = %v", count, truncated)
	}
	if !slices.Equal(tail, []string{"ab", "cd"}) {
		t.Fatalf("tail = %q", tail)
	}
}

func TestOutputWithoutASinkStillCollectsTheTail(t *testing.T) {
	out := newOutput("pre-commit", nil)
	_, _ = out.Write([]byte(strings.Repeat("x\n", 3)))
	if tail, _ := out.finish(); len(tail) != 3 {
		t.Fatalf("tail = %q", tail)
	}
}
