package vault

import (
	"context"
	"errors"
	"testing"
)

func TestPasswordUnlockerRoundTrip(t *testing.T) {
	u := NewPasswordUnlocker([]byte("correct horse battery staple"), TestSlotParams())
	dek := []byte("0123456789abcdef0123456789abcdef")[:32]
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	if slot.Kind != SlotPassword {
		t.Fatalf("kind = %v", slot.Kind)
	}
	got, err := u.Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatalf("unwrapped dek mismatch")
	}
}

func TestPasswordUnlockerWrongPassword(t *testing.T) {
	dek := make([]byte, 32)
	slot, err := NewPasswordUnlocker([]byte("right"), TestSlotParams()).Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPasswordUnlocker([]byte("wrong"), TestSlotParams()).Unwrap(context.Background(), slot)
	if !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestPasswordUnlockerRejectsKindMismatch(t *testing.T) {
	u := NewPasswordUnlocker([]byte("p"), TestSlotParams())
	_, err := u.Unwrap(context.Background(), Slot{Kind: SlotFile})
	if !errors.Is(err, ErrSlotKindMismatch) {
		t.Fatalf("got %v, want ErrSlotKindMismatch", err)
	}
}

func TestPasswordUnlockerKind(t *testing.T) {
	if NewPasswordUnlocker(nil, TestSlotParams()).Kind() != SlotPassword {
		t.Fatal("wrong kind")
	}
}

func TestPasswordUnlockerWrapFailsWhenAEADConstructionFails(t *testing.T) {
	withFailingAEAD(t)
	u := NewPasswordUnlocker([]byte("p"), TestSlotParams())
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); err == nil {
		t.Fatal("expected error")
	}
}

func TestPasswordUnlockerUnwrapFailsWhenAEADConstructionFails(t *testing.T) {
	u := NewPasswordUnlocker([]byte("p"), TestSlotParams())
	slot, err := u.Wrap(context.Background(), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	withFailingAEAD(t)
	if _, err := u.Unwrap(context.Background(), slot); err == nil {
		t.Fatal("expected error")
	}
}
