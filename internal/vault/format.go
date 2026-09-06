package vault

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magicValue              = "GOGITVLT"
	formatVersion    uint16 = 1
	minSlotCount            = 1
	maxSlotCount            = 8
	payloadNonceSize        = chacha20poly1305.NonceSizeX
	slotNonceSize           = chacha20poly1305.NonceSizeX
	headerFixedSize         = len(magicValue) + 2 + 2 + 1
)

var newXChaCha20Poly1305 = chacha20poly1305.NewX

func encodeSlot(s Slot) []byte {
	buf := make([]byte, 0, headerFixedSize+len(s.Kind)+len(s.Salt)+len(s.Nonce)+len(s.Wrapped))
	buf = append(buf, byte(len(s.Kind)))
	buf = append(buf, []byte(s.Kind)...)
	buf = append(buf, byte(len(s.Salt)))
	buf = append(buf, s.Salt...)
	buf = append(buf, byte(len(s.Nonce)))
	buf = append(buf, s.Nonce...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(s.Wrapped)))
	buf = append(buf, s.Wrapped...)
	buf = binary.BigEndian.AppendUint32(buf, s.Params.Time)
	buf = binary.BigEndian.AppendUint32(buf, s.Params.Memory)
	buf = append(buf, s.Params.Threads)
	return buf
}

func readLP8(data []byte, off int) ([]byte, int, error) {
	if off >= len(data) {
		return nil, off, ErrInvalidFormat
	}
	n := int(data[off])
	off++
	if off+n > len(data) {
		return nil, off, ErrInvalidFormat
	}
	return data[off : off+n], off + n, nil
}

func readLP16(data []byte, off int) ([]byte, int, error) {
	if off+2 > len(data) {
		return nil, off, ErrInvalidFormat
	}
	n := int(binary.BigEndian.Uint16(data[off : off+2]))
	off += 2
	if off+n > len(data) {
		return nil, off, ErrInvalidFormat
	}
	return data[off : off+n], off + n, nil
}

func decodeSlot(data []byte) (Slot, int, error) {
	kind, off, err := readLP8(data, 0)
	if err != nil {
		return Slot{}, 0, err
	}
	salt, off, err := readLP8(data, off)
	if err != nil {
		return Slot{}, 0, err
	}
	nonce, off, err := readLP8(data, off)
	if err != nil {
		return Slot{}, 0, err
	}
	wrapped, off, err := readLP16(data, off)
	if err != nil {
		return Slot{}, 0, err
	}
	if off+9 > len(data) {
		return Slot{}, 0, ErrInvalidFormat
	}
	t := binary.BigEndian.Uint32(data[off : off+4])
	m := binary.BigEndian.Uint32(data[off+4 : off+8])
	threads := data[off+8]
	off += 9
	return Slot{
		Kind:    SlotKind(kind),
		Salt:    append([]byte(nil), salt...),
		Nonce:   append([]byte(nil), nonce...),
		Wrapped: append([]byte(nil), wrapped...),
		Params:  SlotParams{Time: t, Memory: m, Threads: threads},
	}, off, nil
}

func encodeHeader(version, flags uint16, slots []Slot) ([]byte, error) {
	if len(slots) < minSlotCount || len(slots) > maxSlotCount {
		return nil, ErrSlotCount
	}
	buf := make([]byte, 0, headerFixedSize+len(slots)*32)
	buf = append(buf, []byte(magicValue)...)
	buf = binary.BigEndian.AppendUint16(buf, version)
	buf = binary.BigEndian.AppendUint16(buf, flags)
	buf = append(buf, byte(len(slots)))
	for _, s := range slots {
		buf = append(buf, encodeSlot(s)...)
	}
	return buf, nil
}

func decodeHeader(data []byte) (version, flags uint16, slots []Slot, consumed int, err error) {
	if len(data) < headerFixedSize {
		return 0, 0, nil, 0, ErrInvalidFormat
	}
	if string(data[:len(magicValue)]) != magicValue {
		return 0, 0, nil, 0, ErrInvalidFormat
	}
	off := len(magicValue)
	version = binary.BigEndian.Uint16(data[off : off+2])
	off += 2
	if version != formatVersion {
		return 0, 0, nil, 0, ErrUnsupportedVersion
	}
	flags = binary.BigEndian.Uint16(data[off : off+2])
	off += 2
	slotCount := int(data[off])
	off++
	if slotCount < minSlotCount || slotCount > maxSlotCount {
		return 0, 0, nil, 0, ErrInvalidFormat
	}
	slots = make([]Slot, 0, slotCount)
	for range slotCount {
		var (
			slot Slot
			n    int
		)
		slot, n, err = decodeSlot(data[off:])
		if err != nil {
			return 0, 0, nil, 0, err
		}
		slots = append(slots, slot)
		off += n
	}
	return version, flags, slots, off, nil
}

func readVaultFile(path string) (version, flags uint16, slots []Slot, nonce, ciphertext []byte, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return 0, 0, nil, nil, nil, fmt.Errorf("vault: %w", readErr)
	}
	version, flags, slots, n, hErr := decodeHeader(data)
	if hErr != nil {
		return 0, 0, nil, nil, nil, hErr
	}
	rest := data[n:]
	if len(rest) <= payloadNonceSize {
		return 0, 0, nil, nil, nil, ErrInvalidFormat
	}
	nonce = append([]byte(nil), rest[:payloadNonceSize]...)
	ciphertext = append([]byte(nil), rest[payloadNonceSize:]...)
	return version, flags, slots, nonce, ciphertext, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	return nil
}
