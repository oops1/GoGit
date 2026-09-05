package vault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const dpapiSaltSize = 32

type DPAPIUnlocker struct{}

func NewDPAPIUnlocker() *DPAPIUnlocker {
	return &DPAPIUnlocker{}
}

func (u *DPAPIUnlocker) Kind() SlotKind {
	return SlotDPAPI
}

func (u *DPAPIUnlocker) Wrap(_ context.Context, dek []byte) (Slot, error) {
	salt := make([]byte, dpapiSaltSize)
	_, _ = rand.Read(salt)
	wrapped, err := dpapiProtect(dek, dpapiEntropy(salt))
	if err != nil {
		return Slot{}, err
	}
	return Slot{
		Kind:    SlotDPAPI,
		Salt:    salt,
		Wrapped: wrapped,
	}, nil
}

func (u *DPAPIUnlocker) Unwrap(_ context.Context, slot Slot) ([]byte, error) {
	if slot.Kind != SlotDPAPI {
		return nil, ErrSlotKindMismatch
	}
	dek, err := dpapiUnprotect(slot.Wrapped, dpapiEntropy(slot.Salt))
	if err != nil {
		return nil, ErrWrongKey
	}
	return dek, nil
}

func dpapiEntropy(salt []byte) []byte {
	h := sha256.New()
	h.Write([]byte(magicValue))
	h.Write(salt)
	return h.Sum(nil)
}

func dpapiProtect(data, entropy []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: dpapiBlobPtr(data)}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: dpapiBlobPtr(entropy)}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	defer dpapiFreeBlob(out)
	return dpapiBlobBytes(out), nil
}

func dpapiUnprotect(data, entropy []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: dpapiBlobPtr(data)}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: dpapiBlobPtr(entropy)}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer dpapiFreeBlob(out)
	return dpapiBlobBytes(out), nil
}

func dpapiBlobPtr(b []byte) *byte {
	if len(b) == 0 {
		return nil
	}
	return &b[0]
}

func dpapiBlobBytes(b windows.DataBlob) []byte {
	if b.Data == nil || b.Size == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(b.Data, int(b.Size))...)
}

func dpapiFreeBlob(b windows.DataBlob) {
	if b.Data == nil {
		return
	}
	clear(unsafe.Slice(b.Data, int(b.Size)))
	_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(b.Data))))
}
