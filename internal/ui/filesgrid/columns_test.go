package filesgrid

import "testing"

func TestDefaultOrderListsEveryKnownColumnOnce(t *testing.T) {
	order := DefaultOrder()
	if len(order) != len(columnDefs) {
		t.Fatalf("len(order) = %d, want %d", len(order), len(columnDefs))
	}
	seen := map[ColumnID]bool{}
	for _, id := range order {
		if seen[id] {
			t.Fatalf("duplicate column %q in DefaultOrder", id)
		}
		seen[id] = true
	}
}

func TestDefaultVisibleReturnsAFreshSliceEachCall(t *testing.T) {
	a := DefaultVisible()
	a[0] = "mutated"
	b := DefaultVisible()
	if b[0] == "mutated" {
		t.Fatal("DefaultVisible must not share backing storage across calls")
	}
}

func TestColumnByIDFindsKnownColumnsAndRejectsUnknown(t *testing.T) {
	if _, ok := columnByID(ColName); !ok {
		t.Fatal("columnByID(ColName) = false, want true")
	}
	if _, ok := columnByID(ColumnID("bogus")); ok {
		t.Fatal("columnByID(bogus) = true, want false")
	}
}

func TestNormalizeOrderDropsUnknownDedupesAndAppendsMissing(t *testing.T) {
	got := NormalizeOrder([]ColumnID{"bogus", ColSize, ColName, ColSize})
	if len(got) != len(columnDefs) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(columnDefs))
	}
	if got[0] != ColSize || got[1] != ColName {
		t.Fatalf("got[:2] = %v, want [Size Name]", got[:2])
	}
	seen := map[ColumnID]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("NormalizeOrder produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestNormalizeOrderOfAnEmptySliceReturnsTheFullDefaultOrder(t *testing.T) {
	got := NormalizeOrder(nil)
	want := DefaultOrder()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeVisibleKeepsOnlyKnownOrderMembersAndDedupes(t *testing.T) {
	order := NormalizeOrder([]ColumnID{ColName, ColState})
	got := NormalizeVisible(order, []ColumnID{"bogus", ColState, ColState, ColSize})
	want := []ColumnID{ColState, ColSize}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got = %v, want %v", got, want)
		}
	}
}

func TestNormalizeVisibleDropsColumnsThatAreNotInOrder(t *testing.T) {
	got := NormalizeVisible([]ColumnID{ColName}, []ColumnID{ColName, ColSize})
	want := []ColumnID{ColName}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got = %v, want %v (Size is not part of the order)", got, want)
	}
}

func TestNormalizeVisibleFallsBackToDefaultWhenNothingSurvives(t *testing.T) {
	order := NormalizeOrder([]ColumnID{ColName})
	got := NormalizeVisible(order, []ColumnID{"bogus"})
	want := DefaultVisible()
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got = %v, want %v", got, want)
		}
	}
}
