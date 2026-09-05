package vault

import "context"

type SlotKind string

const (
	SlotPassword      SlotKind = "password"
	SlotFile          SlotKind = "file"
	SlotDPAPI         SlotKind = "dpapi"
	SlotSecretService SlotKind = "secretservice"
)

type SlotInfo struct {
	Kind  SlotKind
	Index int
}

type SlotParams struct {
	Time    uint32
	Memory  uint32
	Threads uint8
}

type Slot struct {
	Kind    SlotKind
	Salt    []byte
	Nonce   []byte
	Wrapped []byte
	Params  SlotParams
}

type Unlocker interface {
	Kind() SlotKind
	Unwrap(ctx context.Context, slot Slot) ([]byte, error)
	Wrap(ctx context.Context, dek []byte) (Slot, error)
}

func DefaultSlotParams() SlotParams {
	return SlotParams{Time: 3, Memory: 64 * 1024, Threads: 4}
}

func TestSlotParams() SlotParams {
	return SlotParams{Time: 1, Memory: 8 * 1024, Threads: 1}
}

func slotAAD(kind SlotKind, salt []byte) []byte {
	aad := make([]byte, 0, len(kind)+len(salt))
	aad = append(aad, []byte(kind)...)
	aad = append(aad, salt...)
	return aad
}
