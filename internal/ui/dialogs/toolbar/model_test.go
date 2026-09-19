package toolbar

import (
	"slices"
	"testing"
)

func sampleCatalog() []Entry {
	return []Entry{
		{ID: SeparatorID, Label: "——"},
		{ID: StretchID, Label: "<->"},
		{ID: "remote.pull", Label: "Pull"},
		{ID: "remote.push", Label: "Push"},
		{ID: "local.commit", Label: "Commit"},
		{ID: "branch.merge", Label: "Merge"},
	}
}

func sampleDefaults() []string {
	return []string{"remote.pull", SeparatorID, "local.commit"}
}

func newSampleModel() *Model {
	return NewModel(sampleCatalog(), sampleDefaults(), sampleDefaults(), true)
}

func ids(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.ID)
	}
	return out
}

func TestAvailableHidesTheChosenCommandsButKeepsTheSpacers(t *testing.T) {
	m := newSampleModel()
	want := []string{SeparatorID, StretchID, "remote.push", "branch.merge"}
	if got := ids(m.Available()); !slices.Equal(got, want) {
		t.Fatalf("available = %v, want %v", got, want)
	}
	if got := ids(m.Selected()); !slices.Equal(got, sampleDefaults()) {
		t.Fatalf("selected = %v", got)
	}
}

func TestNewModelDropsItemsTheCatalogDoesNotKnowAndRepeatedCommands(t *testing.T) {
	m := NewModel(sampleCatalog(), []string{"no.such.command", "remote.pull", "remote.pull", SeparatorID, SeparatorID}, nil, false)
	want := []string{"remote.pull", SeparatorID, SeparatorID}
	if got := m.Items(); !slices.Equal(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	if m.Captions() {
		t.Fatal("captions must follow the flag they were built with")
	}
	m.SetCaptions(true)
	if !m.Captions() {
		t.Fatal("captions must be settable")
	}
}

func TestAddInsertsTheChosenAvailableEntryAtThePosition(t *testing.T) {
	m := newSampleModel()
	if at := m.Add(2, 1); at != 1 {
		t.Fatalf("insert position = %d, want 1", at)
	}
	want := []string{"remote.pull", "remote.push", SeparatorID, "local.commit"}
	if got := m.Items(); !slices.Equal(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	if slices.Contains(ids(m.Available()), "remote.push") {
		t.Fatal("a chosen command must leave the available list")
	}
}

func TestAddAppendsWhenThePositionIsOutsideTheList(t *testing.T) {
	m := newSampleModel()
	if at := m.Add(0, 99); at != len(sampleDefaults()) {
		t.Fatalf("insert position = %d", at)
	}
	if at := m.Add(1, -1); at != len(sampleDefaults())+1 {
		t.Fatalf("insert position = %d", at)
	}
	want := append(sampleDefaults(), SeparatorID, StretchID)
	if got := m.Items(); !slices.Equal(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
}

func TestAddRefusesAnIndexOutsideTheAvailableList(t *testing.T) {
	m := newSampleModel()
	for _, at := range []int{-1, 99} {
		if got := m.Add(at, 0); got != -1 {
			t.Fatalf("adding available %d returned %d", at, got)
		}
	}
	if got := m.Items(); !slices.Equal(got, sampleDefaults()) {
		t.Fatalf("selected = %v", got)
	}
}

func TestRemoveGivesACommandBackToTheAvailableList(t *testing.T) {
	m := newSampleModel()
	if !m.Remove(0) {
		t.Fatal("the first item must be removable")
	}
	if got := m.Items(); !slices.Equal(got, []string{SeparatorID, "local.commit"}) {
		t.Fatalf("selected = %v", got)
	}
	if !slices.Contains(ids(m.Available()), "remote.pull") {
		t.Fatal("the removed command must come back")
	}
	for _, at := range []int{-1, 99} {
		if m.Remove(at) {
			t.Fatalf("removing %d must fail", at)
		}
	}
}

func TestMoveReordersTheChosenItems(t *testing.T) {
	m := newSampleModel()
	if !m.Move(2, 0) {
		t.Fatal("the last item must be movable to the front")
	}
	if got := m.Items(); !slices.Equal(got, []string{"local.commit", "remote.pull", SeparatorID}) {
		t.Fatalf("selected = %v", got)
	}
	for _, pair := range [][2]int{{-1, 0}, {0, -1}, {0, 99}, {99, 0}, {1, 1}} {
		if m.Move(pair[0], pair[1]) {
			t.Fatalf("moving %v must fail", pair)
		}
	}
}

func TestResetBringsBackTheDefaultRow(t *testing.T) {
	m := newSampleModel()
	m.Remove(0)
	m.Add(0, 0)
	if m.IsDefault() {
		t.Fatal("the row was edited, it cannot be the default one")
	}
	m.Reset()
	if !m.IsDefault() || !slices.Equal(m.Items(), sampleDefaults()) {
		t.Fatalf("after reset selected = %v", m.Items())
	}
	if got := m.Defaults(); !slices.Equal(got, sampleDefaults()) {
		t.Fatalf("defaults = %v", got)
	}
}

func TestARowOfSpacersAloneCarriesNoCommand(t *testing.T) {
	m := NewModel(sampleCatalog(), []string{SeparatorID, StretchID}, sampleDefaults(), true)
	if m.HasCommands() {
		t.Fatal("separators and stretches are not commands")
	}
	m.Add(2, 0)
	if !m.HasCommands() {
		t.Fatal("a command was added")
	}
}

func TestRepeatableNamesTheSpacers(t *testing.T) {
	for _, id := range []string{SeparatorID, StretchID} {
		if !Repeatable(id) {
			t.Fatalf("%q must be repeatable", id)
		}
	}
	if Repeatable("remote.pull") {
		t.Fatal("a command can be placed only once")
	}
}
