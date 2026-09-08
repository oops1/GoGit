package graph

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func id(name string) hash.ObjectID {
	text := fmt.Sprintf("%040x", 0)
	digits := ""
	for _, r := range name {
		digits += fmt.Sprintf("%02x", r)
	}
	parsed, err := hash.Parse(digits + text[len(digits):])
	if err != nil {
		panic(err)
	}
	return parsed
}

func commit(name string, parents ...string) Commit {
	c := Commit{ID: id(name)}
	for _, parent := range parents {
		c.Parents = append(c.Parents, id(parent))
	}
	return c
}

func layoutOf(commits []Commit) []Row {
	layout := New()
	rows := make([]Row, 0, len(commits))
	for _, c := range commits {
		rows = append(rows, layout.Add(c))
	}
	return rows
}

func lanesOf(segments []Segment) []int {
	lanes := make([]int, 0, len(segments))
	for _, segment := range segments {
		lanes = append(lanes, segment.Lane)
	}
	return lanes
}

func sameLanes(got []Segment, want ...int) bool {
	return slices.Equal(lanesOf(got), want)
}

func TestALinearHistoryStaysInOneLane(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("c", "b"),
		commit("b", "a"),
		commit("a"),
	})

	for i, row := range rows {
		if row.Lane != 0 || row.Lanes != 1 || row.Merge {
			t.Fatalf("row %d = %+v, want a single lane", i, row)
		}
	}
	if rows[0].FromAbove || !sameLanes(rows[0].Out, 0) {
		t.Fatalf("first row = %+v, want a line going down alone", rows[0])
	}
	if !rows[1].FromAbove || !sameLanes(rows[1].Out, 0) {
		t.Fatalf("second row = %+v, want the line passing through", rows[1])
	}
	if len(rows[2].Out) != 0 {
		t.Fatalf("root row = %+v, want the line to end", rows[2])
	}
}

func TestAMergeTakesTheLaneOfItsFirstParentAndOpensOneMore(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("m", "b", "c"),
		commit("b", "a"),
		commit("c", "a"),
		commit("a"),
	})

	if !rows[0].Merge || rows[0].Lane != 0 {
		t.Fatalf("merge row = %+v, want a merge dot in the first lane", rows[0])
	}
	if !sameLanes(rows[0].Out, 0, 1) {
		t.Fatalf("merge out = %v, want both parents", lanesOf(rows[0].Out))
	}
	if rows[0].Lanes != 2 {
		t.Fatalf("lanes = %d, want two", rows[0].Lanes)
	}
	if rows[1].Lane != 0 || !sameLanes(rows[1].Through, 1) {
		t.Fatalf("row of the first parent = %+v, want the second lane passing by", rows[1])
	}
}

func TestTwoBranchesThatMeetAgainCollapseIntoOneLane(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("m", "b", "c"),
		commit("b", "a"),
		commit("c", "a"),
		commit("a"),
	})

	if !sameLanes(rows[2].Out, 0) {
		t.Fatalf("second parent goes to %v, want the lane its parent already holds", lanesOf(rows[2].Out))
	}
	if !sameLanes(rows[2].Through, 0) {
		t.Fatalf("through = %v, want the first lane to keep going", lanesOf(rows[2].Through))
	}
	if rows[3].Lane != 0 || rows[3].Lanes != 1 {
		t.Fatalf("root row = %+v, want one lane left", rows[3])
	}
}

func TestAMergeOfThreeParentsOpensTwoMoreLanes(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("m", "a", "b", "c"),
		commit("a"),
		commit("b"),
		commit("c"),
	})

	if !sameLanes(rows[0].Out, 0, 1, 2) {
		t.Fatalf("out = %v, want a lane per parent", lanesOf(rows[0].Out))
	}
	if rows[0].Lanes != 3 {
		t.Fatalf("lanes = %d, want three", rows[0].Lanes)
	}
	for i, want := range []int{0, 1, 2} {
		if rows[i+1].Lane != want {
			t.Fatalf("parent %d sits in lane %d, want %d", i, rows[i+1].Lane, want)
		}
	}
}

func TestATipJoinsTheLaneThatAlreadyWaitsForItsParent(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("x", "m"),
		commit("y", "m"),
		commit("m", "a"),
		commit("a"),
	})

	if rows[1].Lane != 1 || !sameLanes(rows[1].Out, 0) {
		t.Fatalf("row = %+v, want the second tip to lead into the waiting lane", rows[1])
	}
	if rows[1].Lanes != 2 {
		t.Fatalf("lanes = %d, want room for both the dot and the lane it leads to", rows[1].Lanes)
	}
	if rows[2].Lane != 0 || !rows[2].FromAbove || rows[2].Lanes != 1 {
		t.Fatalf("row = %+v, want the shared parent alone in the first lane", rows[2])
	}
}

func TestBranchesConvergeWhereTheyShareACommitNotAtTheDot(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("t", "x", "y"),
		commit("x", "m"),
		commit("y", "m"),
		commit("m", "a"),
		commit("a"),
	})

	if !sameLanes(rows[2].Out, 0) || rows[2].Lane != 1 {
		t.Fatalf("row = %+v, want the second branch to bend into the lane of the shared commit", rows[2])
	}
	if rows[3].Lane != 0 || !rows[3].FromAbove || len(rows[3].Through) != 0 {
		t.Fatalf("row = %+v, want one line reaching the shared commit", rows[3])
	}
	if rows[4].Lanes != 1 {
		t.Fatalf("lanes = %d, want the freed lane to be gone", rows[4].Lanes)
	}
}

func TestTwoIndependentHistoriesGetTheirOwnLanes(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("a1", "a0"),
		commit("b1", "b0"),
		commit("a0"),
		commit("b0"),
	})

	if rows[0].Lane != 0 || rows[1].Lane != 1 {
		t.Fatalf("lanes = %d and %d, want one per history", rows[0].Lane, rows[1].Lane)
	}
	if rows[0].Color == rows[1].Color {
		t.Fatalf("colour %d used twice, want a colour per lane", rows[0].Color)
	}
	if !sameLanes(rows[2].Through, 1) {
		t.Fatalf("through = %v, want the other history to pass by", lanesOf(rows[2].Through))
	}
}

func TestAFreedLaneIsTakenByTheNextTip(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("a1", "a0"),
		commit("b1", "b0"),
		commit("b0"),
		commit("m", "a0", "c0"),
	})

	if rows[3].Lane != 1 {
		t.Fatalf("merge lane = %d, want the lane freed by the finished branch", rows[3].Lane)
	}
	if !sameLanes(rows[3].Out, 0, 1) {
		t.Fatalf("out = %v, want the first parent in the lane that waits for it", lanesOf(rows[3].Out))
	}
	if !rows[3].Merge {
		t.Fatalf("row = %+v, want a merge dot", rows[3])
	}
}

func TestALaneKeepsItsColourWhileItLives(t *testing.T) {
	rows := layoutOf([]Commit{
		commit("m", "b", "c"),
		commit("b", "a"),
		commit("c", "a"),
		commit("a"),
	})

	first := rows[0].Color
	for i, row := range rows {
		if row.Lane == 0 && row.Color != first {
			t.Fatalf("row %d in lane 0 has colour %d, want %d for the whole life of the lane", i, row.Color, first)
		}
	}
	if rows[2].Color == first {
		t.Fatalf("the second lane has colour %d, want one of its own", rows[2].Color)
	}
}

func TestTheLayoutOfAPageDoesNotDependOnWhereThePageEnds(t *testing.T) {
	commits := []Commit{
		commit("m", "b", "c"),
		commit("b", "a"),
		commit("c", "a"),
		commit("a", "root"),
		commit("root"),
	}

	whole := layoutOf(commits)

	layout := New()
	var split []Row
	for _, page := range [][]Commit{commits[:2], commits[2:4], commits[4:]} {
		for _, c := range page {
			split = append(split, layout.Add(c))
		}
	}

	for i := range whole {
		if fmt.Sprint(whole[i]) != fmt.Sprint(split[i]) {
			t.Fatalf("row %d: page by page = %+v, all at once = %+v", i, split[i], whole[i])
		}
	}
}

func TestResetForgetsTheLanesOfTheOldRepository(t *testing.T) {
	layout := New()
	layout.Add(commit("m", "b", "c"))

	layout.Reset()
	row := layout.Add(commit("z", "y"))

	if row.Lane != 0 || row.Lanes != 1 || len(row.Through) != 0 {
		t.Fatalf("row = %+v, want a layout that starts over", row)
	}
}

func TestLanesBeyondTheLimitCollapseIntoTheLastOne(t *testing.T) {
	layout := New()
	layout.SetLimit(3)
	parents := []string{}
	for i := range 6 {
		parents = append(parents, fmt.Sprintf("p%d", i))
	}
	row := layout.Add(commit("m", parents...))

	if !row.Overflow {
		t.Fatalf("row = %+v, want the overflow reported", row)
	}
	if row.Lanes != 3 {
		t.Fatalf("lanes = %d, want the limit", row.Lanes)
	}
	for _, segment := range row.Out {
		if segment.Lane > 2 {
			t.Fatalf("out = %v, want every lane inside the limit", lanesOf(row.Out))
		}
	}
}

func TestALimitBelowOneIsIgnored(t *testing.T) {
	layout := New()
	layout.SetLimit(0)

	if layout.limit != MaxLanes {
		t.Fatalf("limit = %d, want the default kept", layout.limit)
	}
}

func TestEveryParentIsDrawnIntoTheLaneWhereItLaterSits(t *testing.T) {
	commits := []Commit{
		commit("m", "b", "c"),
		commit("b", "a"),
		commit("c", "a"),
		commit("a", "root"),
		commit("root"),
	}
	rows := layoutOf(commits)

	lanes := map[string]int{}
	for i, c := range commits {
		lanes[nameOf(c)] = rows[i].Lane
	}
	for i, c := range commits {
		for at, parent := range c.Parents {
			want := lanes[nameOfID(commits, parent)]
			if !slices.Contains(lanesOf(rows[i].Out), want) {
				t.Fatalf("commit %d parent %d leads to %v, want the lane %d where the parent sits",
					i, at, lanesOf(rows[i].Out), want)
			}
		}
	}
}

func nameOf(c Commit) string {
	return c.ID.String()
}

func nameOfID(commits []Commit, id hash.ObjectID) string {
	for _, c := range commits {
		if c.ID == id {
			return c.ID.String()
		}
	}
	return strings.Repeat("0", 40)
}
