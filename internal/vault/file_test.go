package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileUnlockerCreatesKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "nested", "vault.key")
	u := NewFileUnlocker(keyPath)
	dek := make([]byte, 32)
	if _, err := u.Wrap(context.Background(), dek); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 32 {
		t.Fatalf("key file length = %d, want 32", len(data))
	}
}

func TestFileUnlockerRoundTrip(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	u := NewFileUnlocker(keyPath)
	dek := []byte("0123456789abcdef0123456789abcdef")[:32]
	slot, err := u.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := u.Unwrap(context.Background(), slot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatal("unwrapped dek mismatch")
	}
}

func TestFileUnlockerReusesExistingKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	u := NewFileUnlocker(keyPath)
	dek := make([]byte, 32)
	if _, err := u.Wrap(context.Background(), dek); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Wrap(context.Background(), dek); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("key file was regenerated instead of reused")
	}
}

func TestFileUnlockerWrongKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	dek := make([]byte, 32)
	slot, err := NewFileUnlocker(keyPath).Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	otherKey := filepath.Join(t.TempDir(), "vault.key")
	if _, err := NewFileUnlocker(otherKey).Wrap(context.Background(), make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	_, err = NewFileUnlocker(otherKey).Unwrap(context.Background(), slot)
	if !errors.Is(err, ErrWrongKey) {
		t.Fatalf("got %v, want ErrWrongKey", err)
	}
}

func TestFileUnlockerInvalidKeyFileSize(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(keyPath, []byte("too short"), 0o600); err != nil {
		t.Fatal(err)
	}
	u := NewFileUnlocker(keyPath)
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); !errors.Is(err, ErrInvalidKeyFile) {
		t.Fatalf("Wrap: got %v, want ErrInvalidKeyFile", err)
	}
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotFile}); !errors.Is(err, ErrInvalidKeyFile) {
		t.Fatalf("Unwrap: got %v, want ErrInvalidKeyFile", err)
	}
}

func TestFileUnlockerUnwrapMissingKeyFile(t *testing.T) {
	u := NewFileUnlocker(filepath.Join(t.TempDir(), "missing.key"))
	if _, err := u.Unwrap(context.Background(), Slot{Kind: SlotFile}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFileUnlockerRejectsKindMismatch(t *testing.T) {
	u := NewFileUnlocker(filepath.Join(t.TempDir(), "vault.key"))
	_, err := u.Unwrap(context.Background(), Slot{Kind: SlotPassword})
	if !errors.Is(err, ErrSlotKindMismatch) {
		t.Fatalf("got %v, want ErrSlotKindMismatch", err)
	}
}

func TestFileUnlockerKind(t *testing.T) {
	if NewFileUnlocker("x").Kind() != SlotFile {
		t.Fatal("wrong kind")
	}
}

func TestFileUnlockerWrapFailsWhenKeyPathIsDirectory(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := os.Mkdir(keyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	u := NewFileUnlocker(keyPath)
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); err == nil {
		t.Fatal("expected error")
	}
}

func TestFileUnlockerWrapFailsWhenAEADConstructionFails(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	withFailingAEAD(t)
	u := NewFileUnlocker(keyPath)
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); err == nil {
		t.Fatal("expected error")
	}
}

func TestFileUnlockerUnwrapFailsWhenAEADConstructionFails(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	u := NewFileUnlocker(keyPath)
	slot, err := u.Wrap(context.Background(), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	withFailingAEAD(t)
	if _, err := u.Unwrap(context.Background(), slot); err == nil {
		t.Fatal("expected error")
	}
}

func TestFileUnlockerWrapFailsWhenDirBlocked(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	u := NewFileUnlocker(filepath.Join(blocker, "vault.key"))
	if _, err := u.Wrap(context.Background(), make([]byte, 32)); err == nil {
		t.Fatal("expected error")
	}
}
