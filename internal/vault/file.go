package vault

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/crypto/chacha20poly1305"
)

type FileUnlocker struct {
	path string
}

func NewFileUnlocker(path string) *FileUnlocker {
	return &FileUnlocker{path: path}
}

func (u *FileUnlocker) Kind() SlotKind {
	return SlotFile
}

func (u *FileUnlocker) Wrap(_ context.Context, dek []byte) (Slot, error) {
	kek, err := u.loadOrCreateKey()
	if err != nil {
		return Slot{}, err
	}
	defer clear(kek)
	nonce := make([]byte, slotNonceSize)
	_, _ = rand.Read(nonce)
	aead, err := newXChaCha20Poly1305(kek)
	if err != nil {
		return Slot{}, err
	}
	wrapped := aead.Seal(nil, nonce, dek, slotAAD(SlotFile, nil))
	return Slot{
		Kind:    SlotFile,
		Nonce:   nonce,
		Wrapped: wrapped,
	}, nil
}

func (u *FileUnlocker) Unwrap(_ context.Context, slot Slot) ([]byte, error) {
	if slot.Kind != SlotFile {
		return nil, ErrSlotKindMismatch
	}
	kek, err := u.readKey()
	if err != nil {
		return nil, err
	}
	defer clear(kek)
	aead, err := newXChaCha20Poly1305(kek)
	if err != nil {
		return nil, err
	}
	dek, err := aead.Open(nil, slot.Nonce, slot.Wrapped, slotAAD(SlotFile, slot.Salt))
	if err != nil {
		return nil, ErrWrongKey
	}
	return dek, nil
}

func (u *FileUnlocker) loadOrCreateKey() ([]byte, error) {
	data, err := os.ReadFile(u.path)
	if errors.Is(err, fs.ErrNotExist) {
		key := make([]byte, chacha20poly1305.KeySize)
		_, _ = rand.Read(key)
		if err := os.MkdirAll(filepath.Dir(u.path), 0o700); err != nil {
			return nil, fmt.Errorf("vault: %w", err)
		}
		if err := os.WriteFile(u.path, key, 0o600); err != nil {
			return nil, fmt.Errorf("vault: %w", err)
		}
		return key, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	if len(data) != chacha20poly1305.KeySize {
		clear(data)
		return nil, ErrInvalidKeyFile
	}
	return data, nil
}

func (u *FileUnlocker) readKey() ([]byte, error) {
	data, err := os.ReadFile(u.path)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	if len(data) != chacha20poly1305.KeySize {
		clear(data)
		return nil, ErrInvalidKeyFile
	}
	return data, nil
}
