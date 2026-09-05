package vault

import "testing"

func TestSlotParamsDefaults(t *testing.T) {
	d := DefaultSlotParams()
	if d.Time != 3 || d.Memory != 64*1024 || d.Threads != 4 {
		t.Fatalf("got %+v", d)
	}
	ts := TestSlotParams()
	if ts.Time != 1 || ts.Memory != 8*1024 || ts.Threads != 1 {
		t.Fatalf("got %+v", ts)
	}
}

func TestSlotAADConcatenatesKindAndSalt(t *testing.T) {
	got := slotAAD(SlotPassword, []byte{1, 2, 3})
	want := append([]byte(SlotPassword), 1, 2, 3)
	if string(got) != string(want) {
		t.Fatalf("got %x want %x", got, want)
	}
}
