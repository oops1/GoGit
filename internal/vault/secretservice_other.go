//go:build !linux

package vault

import "context"

func SecretServiceAvailable() bool {
	return false
}

type SecretServiceUnlocker struct{}

func NewSecretServiceUnlocker(_ string) *SecretServiceUnlocker {
	return &SecretServiceUnlocker{}
}

func (u *SecretServiceUnlocker) Kind() SlotKind {
	return SlotSecretService
}

func (u *SecretServiceUnlocker) Wrap(_ context.Context, _ []byte) (Slot, error) {
	return Slot{}, ErrSlotUnavailable
}

func (u *SecretServiceUnlocker) Unwrap(_ context.Context, _ Slot) ([]byte, error) {
	return nil, ErrSlotUnavailable
}
