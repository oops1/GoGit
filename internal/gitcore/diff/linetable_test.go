package diff

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func revisionsOfAFile(count, lines int) [][]byte {
	text := make([]string, lines)
	for at := range text {
		text[at] = fmt.Sprintf("line %d of the file\n", at)
	}
	out := make([][]byte, 0, count)
	for at := range count {
		text[(at*7)%lines] = fmt.Sprintf("line touched by revision %d\n", at)
		if at%3 == 0 {
			text = append(text, fmt.Sprintf("appended by revision %d\n", at))
		}
		out = append(out, []byte(strings.Join(text, "")))
	}
	return out
}

func TestALineTableGivesTheSameChangesAsAFreshOne(t *testing.T) {
	revisions := revisionsOfAFile(12, 40)
	for _, algorithm := range []Algorithm{AlgorithmMyers, AlgorithmMinimal, AlgorithmPatience, AlgorithmHistogram} {
		for _, ws := range []Whitespace{0, IgnoreAllSpace, IgnoreSpaceChange} {
			t.Run(fmt.Sprintf("%v/%v", algorithm, ws), func(t *testing.T) {
				opts := Defaults()
				opts.Algorithm, opts.IgnoreWhitespace = algorithm, ws
				shared := opts
				shared.Lines = NewLineTable()
				for at := 1; at < len(revisions); at++ {
					older, newer := splitLines(revisions[at-1]), splitLines(revisions[at])
					want := ChangesOfText(older, newer, opts)
					got := ChangesOfText(older, newer, shared)
					if !slices.Equal(got, want) {
						t.Fatalf("revision %d: %+v with a shared table, %+v without", at, got, want)
					}
				}
			})
		}
	}
}

func TestALineTableKeepsItsBuffersBetweenCalls(t *testing.T) {
	revisions := revisionsOfAFile(6, 30)
	opts := Defaults()
	opts.Lines = NewLineTable()

	for at := 1; at < len(revisions); at++ {
		ChangesOfText(splitLines(revisions[at-1]), splitLines(revisions[at]), opts)
	}

	table := opts.Lines
	if len(table.cf.ids) == 0 {
		t.Fatal("the table learned no lines")
	}
	if cap(table.kvalues) == 0 || cap(table.scratch[0].ids) == 0 || cap(table.scratch[1].rchg) == 0 {
		t.Fatalf("the table kept no buffers: %d %d %d", cap(table.kvalues), cap(table.scratch[0].ids), cap(table.scratch[1].rchg))
	}
	if cap(table.changes) == 0 {
		t.Fatal("the table kept no room for the change list")
	}
}

func TestALineTableCountsTheLinesOfEachPairOnItsOwn(t *testing.T) {
	opts := Defaults()
	opts.Lines = NewLineTable()
	first := []byte("a\na\na\nb\n")
	second := []byte("a\nb\n")

	ChangesOfText(splitLines(first), splitLines(first), opts)
	got := ChangesOfText(splitLines(second), splitLines(first), opts)

	fresh := ChangesOfText(splitLines(second), splitLines(first), Defaults())
	if !slices.Equal(got, fresh) {
		t.Fatalf("counts leaked between calls: %+v, want %+v", got, fresh)
	}
}

func TestChangesOfTextAgreesWithChanges(t *testing.T) {
	revisions := revisionsOfAFile(8, 25)
	opts := Defaults()
	for at := 1; at < len(revisions); at++ {
		older, newer := revisions[at-1], revisions[at]
		want := Changes(older, newer, opts)
		got := ChangesOfText(splitLines(older), splitLines(newer), opts)
		if !slices.Equal(got, want) {
			t.Fatalf("revision %d: lines gave %+v, bytes gave %+v", at, got, want)
		}
	}
}
