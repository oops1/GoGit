package graph

import (
	"strings"
	"testing"
)

func picture(rows []Row) string {
	var out []string
	for _, row := range rows {
		out = append(out, dotLine(row), edgeLine(row))
	}
	return strings.Join(out, "\n")
}

func dotLine(row Row) string {
	line := blank(row.Lanes)
	for _, segment := range row.Through {
		line[segment.Lane] = '|'
	}
	line[row.Lane] = '*'
	if row.Merge {
		line[row.Lane] = 'o'
	}
	return string(line)
}

func edgeLine(row Row) string {
	line := blank(row.Lanes)
	for _, segment := range row.Through {
		line[segment.Lane] = '|'
	}
	for _, segment := range row.Out {
		switch {
		case segment.Lane == row.Lane:
			line[segment.Lane] = '|'
		case segment.Lane > row.Lane:
			line[segment.Lane] = '\\'
		default:
			line[segment.Lane] = '/'
		}
	}
	return string(line)
}

func blank(lanes int) []rune {
	line := make([]rune, lanes)
	for i := range line {
		line[i] = ' '
	}
	return line
}

func TestTheGraphOfATangledHistoryIsDrawnLikeThis(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("m", "b", "c"),
		commit("b", "d"),
		commit("c", "d"),
		commit("d", "e"),
		commit("e", "f", "g"),
		commit("f", "h"),
		commit("g", "h"),
		commit("h"),
	})

	want := strings.Join([]string{
		"o ",
		"|\\",
		"*|",
		"||",
		"|*",
		"/ ",
		"*",
		"|",
		"o ",
		"|\\",
		"*|",
		"||",
		"|*",
		"/ ",
		"*",
		" ",
	}, "\n")

	if got := picture(rows); got != want {
		t.Fatalf("the graph is drawn as\n%s\nwant\n%s", got, want)
	}
}

func TestTheGraphOfALongRunningBranchIsDrawnLikeThis(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("m", "a3", "b2"),
		commit("a3", "a2"),
		commit("b2", "b1"),
		commit("a2", "a1"),
		commit("b1", "a1"),
		commit("a1"),
	})

	want := strings.Join([]string{
		"o ",
		"|\\",
		"*|",
		"||",
		"|*",
		"||",
		"*|",
		"||",
		"|*",
		"/ ",
		"*",
		" ",
	}, "\n")

	if got := picture(rows); got != want {
		t.Fatalf("the graph is drawn as\n%s\nwant\n%s", got, want)
	}
}
