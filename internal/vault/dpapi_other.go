//go:build !windows

package vault

import "context"

type DPAPIUnlocker struct{}

func NewDPAPIUnlocker() *DPAPIUnlocker {
	return &DPAPIUnlocker{}
}

func (u *DPAPIUnlocker) Kind() SlotKind {
	return SlotDPAPI
}

func (u *DPAPIUnlocker) Wrap(_ context.Context, _ []byte) (Slot, error) {
	return Slot{}, ErrSlotUnavailable
}

func (u *DPAPIUnlocker) Unwrap(_ context.Context, _ Slot) ([]byte, error) {
	return nil, ErrSlotUnavailable
}
