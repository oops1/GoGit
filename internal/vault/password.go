package vault

import (
	"context"
	"crypto/rand"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const passwordSaltSize = 32

type PasswordUnlocker struct {
	password []byte
	params   SlotParams
}

func NewPasswordUnlocker(password []byte, params SlotParams) *PasswordUnlocker {
	return &PasswordUnlocker{
		password: append([]byte(nil), password...),
		params:   params,
	}
}

func (u *PasswordUnlocker) Kind() SlotKind {
	return SlotPassword
}

func (u *PasswordUnlocker) Wrap(_ context.Context, dek []byte) (Slot, error) {
	salt := make([]byte, passwordSaltSize)
	_, _ = rand.Read(salt)
	nonce := make([]byte, slotNonceSize)
	_, _ = rand.Read(nonce)
	kek := deriveKey(u.password, salt, u.params)
	defer clear(kek)
	aead, err := newXChaCha20Poly1305(kek)
	if err != nil {
		return Slot{}, err
	}
	wrapped := aead.Seal(nil, nonce, dek, slotAAD(SlotPassword, salt))
	return Slot{
		Kind:    SlotPassword,
		Salt:    salt,
		Nonce:   nonce,
		Wrapped: wrapped,
		Params:  u.params,
	}, nil
}

func (u *PasswordUnlocker) Unwrap(_ context.Context, slot Slot) ([]byte, error) {
	if slot.Kind != SlotPassword {
		return nil, ErrSlotKindMismatch
	}
	kek := deriveKey(u.password, slot.Salt, slot.Params)
	defer clear(kek)
	aead, err := newXChaCha20Poly1305(kek)
	if err != nil {
		return nil, err
	}
	dek, err := aead.Open(nil, slot.Nonce, slot.Wrapped, slotAAD(SlotPassword, slot.Salt))
	if err != nil {
		return nil, ErrWrongKey
	}
	return dek, nil
}

func deriveKey(password, salt []byte, params SlotParams) []byte {
	return argon2.IDKey(password, salt, params.Time, params.Memory, params.Threads, chacha20poly1305.KeySize)
}
