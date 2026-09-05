//go:build linux

package vault

func DefaultSlotKind(secretServiceAvailable func() bool) SlotKind {
	if secretServiceAvailable != nil && secretServiceAvailable() {
		return SlotSecretService
	}
	return SlotPassword
}
