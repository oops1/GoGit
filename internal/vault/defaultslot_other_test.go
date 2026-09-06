//go:build !windows && !linux

package vault

import "testing"

func TestDefaultSlotKindIsPasswordElsewhere(t *testing.T) {
	if got := DefaultSlotKind(func() bool { return true }); got != SlotPassword {
		t.Fatalf("got %v, want %v", got, SlotPassword)
	}
}
