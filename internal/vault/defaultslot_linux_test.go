//go:build linux

package vault

import "testing"

func TestDefaultSlotKindPrefersSecretServiceWhenAvailable(t *testing.T) {
	if got := DefaultSlotKind(func() bool { return true }); got != SlotSecretService {
		t.Fatalf("got %v, want %v", got, SlotSecretService)
	}
}

func TestDefaultSlotKindFallsBackToPasswordWhenUnavailable(t *testing.T) {
	if got := DefaultSlotKind(func() bool { return false }); got != SlotPassword {
		t.Fatalf("got %v, want %v", got, SlotPassword)
	}
}

func TestDefaultSlotKindFallsBackToPasswordWhenNil(t *testing.T) {
	if got := DefaultSlotKind(nil); got != SlotPassword {
		t.Fatalf("got %v, want %v", got, SlotPassword)
	}
}
