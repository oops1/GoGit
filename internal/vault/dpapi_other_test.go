//go:build !windows

package vault

import (
	"context"
	"errors"
	"testing"
)

func TestDPAPIUnlockerUnavailableOutsideWindows(t *testing.T) {
	u := NewDPAPIUnlocker()
	if u.Kind() != SlotDPAPI {
		t.Fatal("wrong kind")
	}
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("Wrap: got %v, want ErrSlotUnavailable", err)
	}
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotDPAPI}); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("Unwrap: got %v, want ErrSlotUnavailable", err)
	}
}
