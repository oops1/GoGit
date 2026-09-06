package vault

func DefaultSlotKind(_ func() bool) SlotKind {
	return SlotDPAPI
}
