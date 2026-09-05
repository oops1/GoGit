package vault

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDPAPIUnlockerRoundTrip(t *testing.T) {
	u := NewDPAPIUnlocker()
	dek := []byte("0123456789abcdef0123456789abcdef")[:32]
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	if slot.Kind != SlotDPAPI {
		t.Fatalf("kind = %v", slot.Kind)
	}
	got, err := u.Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatal("unwrapped dek mismatch")
	}
}

func TestDPAPIUnlockerRejectsCorruptedWrapper(t *testing.T) {
	u := NewDPAPIUnlocker()
	dek := make([]byte, 32)
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	slot.Wrapped = append([]byte(nil), slot.Wrapped...)
	slot.Wrapped[len(slot.Wrapped)/2] ^= 0xFF
	if _, err := u.Unwrap(context.Background(), slot); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestDPAPIUnlockerRejectsForeignEntropy(t *testing.T) {
	u := NewDPAPIUnlocker()
	dek := make([]byte, 32)
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	slot.Salt = append([]byte(nil), slot.Salt...)
	slot.Salt[0] ^= 0xFF
	if _, err := u.Unwrap(context.Background(), slot); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestDPAPIUnlockerRejectsKindMismatch(t *testing.T) {
	u := NewDPAPIUnlocker()
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotFile}); !errors.Is(err, ErrSlotKindMismatch) {
		t.Fatalf("got %v, want ErrSlotKindMismatch", err)
	}
}

func TestDPAPIUnlockerKind(t *testing.T) {
	if NewDPAPIUnlocker().Kind() != SlotDPAPI {
		t.Fatal("wrong kind")
	}
}

func TestDPAPIEntropyDependsOnSalt(t *testing.T) {
	a := dpapiEntropy([]byte{1, 2, 3})
	b := dpapiEntropy([]byte{1, 2, 4})
	if string(a) == string(b) {
		t.Fatal("entropy must depend on the salt")
	}
}

func TestDPAPIBlobBytesHandlesEmptyBlob(t *testing.T) {
	if got := dpapiBlobBytes(windows.DataBlob{}); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestDPAPIBlobPtrHandlesEmptySlice(t *testing.T) {
	if got := dpapiBlobPtr(nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestDPAPIFreeBlobIgnoresEmptyBlob(t *testing.T) {
	dpapiFreeBlob(windows.DataBlob{})
}

func TestDPAPIProtectFailsOnEmptyInput(t *testing.T) {
	if _, err := dpapiProtect(nil, dpapiEntropy([]byte("salt"))); err == nil {
		t.Fatal("expected error for empty input blob")
	}
}

func TestDPAPIUnlockerWrapFailsWhenProtectFails(t *testing.T) {
	u := NewDPAPIUnlocker()
	if _, err := u.Wrap(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty dek")
	}
}
