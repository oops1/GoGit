//go:build !windows && !linux

package vault

func DefaultSlotKind(_ func() bool) SlotKind {
	return SlotPassword
}
