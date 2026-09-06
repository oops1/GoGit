package vault

import "testing"

func TestDefaultSlotKindIsDPAPIOnWindows(t *testing.T) {
	if got := DefaultSlotKind(nil); got != SlotDPAPI {
		t.Fatalf("got %v, want %v", got, SlotDPAPI)
	}
	if got := DefaultSlotKind(func() bool { return true }); got != SlotDPAPI {
		t.Fatalf("got %v, want %v", got, SlotDPAPI)
	}
}
