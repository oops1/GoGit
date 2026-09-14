package vault

import (
	"crypto/sha256"
	"encoding/binary"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magicValue                     = "GOGITVLT"
	formatVersionUnnumbered uint16 = 1
	formatVersion           uint16 = 2
	minSlotCount                   = 1
	maxSlotCount                   = 8
	payloadNonceSize               = chacha20poly1305.NonceSizeX
	slotNonceSize                  = chacha20poly1305.NonceSizeX
	headerFixedSize                = len(magicValue) + 2 + 2 + 1
	generationSize                 = 8
)

var newXChaCha20Poly1305 = chacha20poly1305.NewX

type header struct {
	version    uint16
	flags      uint16
	generation uint64
	slots      []Slot
}

type vaultFile struct {
	header     header
	headerRaw  []byte
	nonce      []byte
	ciphertext []byte
	sum        [sha256.Size]byte
}

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
	params := SlotParams{
		Time:    binary.BigEndian.Uint32(data[off : off+4]),
		Memory:  binary.BigEndian.Uint32(data[off+4 : off+8]),
		Threads: data[off+8],
	}
	off += 9
	if SlotKind(kind) == SlotPassword && !params.withinBounds() {
		return Slot{}, 0, ErrInvalidFormat
	}
	return Slot{
		Kind:    SlotKind(kind),
		Salt:    append([]byte(nil), salt...),
		Nonce:   append([]byte(nil), nonce...),
		Wrapped: append([]byte(nil), wrapped...),
		Params:  params,
	}, off, nil
}

const (
	minSlotTime    = 1
	maxSlotTime    = 10
	minSlotMemory  = 8 * 1024
	maxSlotMemory  = 1024 * 1024
	minSlotThreads = 1
	maxSlotThreads = 16
)

func (p SlotParams) withinBounds() bool {
	return p.Time >= minSlotTime && p.Time <= maxSlotTime &&
		p.Memory >= minSlotMemory && p.Memory <= maxSlotMemory &&
		p.Threads >= minSlotThreads && p.Threads <= maxSlotThreads
}

func encodeHeader(h header) ([]byte, error) {
	if len(h.slots) < minSlotCount || len(h.slots) > maxSlotCount {
		return nil, ErrSlotCount
	}
	buf := make([]byte, 0, headerFixedSize+generationSize+len(h.slots)*32)
	buf = append(buf, []byte(magicValue)...)
	buf = binary.BigEndian.AppendUint16(buf, h.version)
	buf = binary.BigEndian.AppendUint16(buf, h.flags)
	if h.version != formatVersionUnnumbered {
		buf = binary.BigEndian.AppendUint64(buf, h.generation)
	}
	buf = append(buf, byte(len(h.slots)))
	for _, s := range h.slots {
		buf = append(buf, encodeSlot(s)...)
	}
	return buf, nil
}

func decodeHeader(data []byte) (header, int, error) {
	if len(data) < headerFixedSize || string(data[:len(magicValue)]) != magicValue {
		return header{}, 0, ErrInvalidFormat
	}
	off := len(magicValue)
	h := header{version: binary.BigEndian.Uint16(data[off : off+2])}
	off += 2
	if h.version != formatVersion && h.version != formatVersionUnnumbered {
		return header{}, 0, ErrUnsupportedVersion
	}
	h.flags = binary.BigEndian.Uint16(data[off : off+2])
	off += 2
	if h.version == formatVersion {
		if len(data) < off+generationSize+1 {
			return header{}, 0, ErrInvalidFormat
		}
		h.generation = binary.BigEndian.Uint64(data[off : off+generationSize])
		off += generationSize
	}
	slotCount := int(data[off])
	off++
	if slotCount < minSlotCount || slotCount > maxSlotCount {
		return header{}, 0, ErrInvalidFormat
	}
	h.slots = make([]Slot, 0, slotCount)
	for range slotCount {
		slot, n, err := decodeSlot(data[off:])
		if err != nil {
			return header{}, 0, err
		}
		h.slots = append(h.slots, slot)
		off += n
	}
	return h, off, nil
}

func parseVaultFile(data []byte) (vaultFile, error) {
	h, n, err := decodeHeader(data)
	if err != nil {
		return vaultFile{}, err
	}
	rest := data[n:]
	if len(rest) <= payloadNonceSize {
		return vaultFile{}, ErrInvalidFormat
	}
	return vaultFile{
		header:     h,
		headerRaw:  append([]byte(nil), data[:n]...),
		nonce:      append([]byte(nil), rest[:payloadNonceSize]...),
		ciphertext: append([]byte(nil), rest[payloadNonceSize:]...),
		sum:        sha256.Sum256(data),
	}, nil
}
