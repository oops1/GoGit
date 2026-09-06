//go:build !linux

package vault

import (
	"context"
	"errors"
	"testing"
)

func TestSecretServiceAvailableIsFalseOutsideLinux(t *testing.T) {
	if SecretServiceAvailable() {
		t.Fatal("secret service cannot be available outside linux")
	}
}

func TestSecretServiceUnlockerUnavailableOutsideLinux(t *testing.T) {
	u := NewSecretServiceUnlocker("gogit")
	if u.Kind() != SlotSecretService {
		t.Fatal("wrong kind")
	}
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("Wrap: got %v, want ErrSlotUnavailable", err)
	}
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotSecretService}); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("Unwrap: got %v, want ErrSlotUnavailable", err)
	}
}
