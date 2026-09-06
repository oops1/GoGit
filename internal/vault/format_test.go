package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fixedGoldenSlot() Slot {
	salt := make([]byte, 32)
	nonce := make([]byte, 24)
	wrapped := make([]byte, 48)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	for i := range nonce {
		nonce[i] = byte(i + 100)
	}
	for i := range wrapped {
		wrapped[i] = byte(i + 200)
	}
	return Slot{
		Kind:    SlotPassword,
		Salt:    salt,
		Nonce:   nonce,
		Wrapped: wrapped,
		Params:  SlotParams{Time: 3, Memory: 65536, Threads: 4},
	}
}

func TestSlotEncodingGolden(t *testing.T) {
	golden := filepath.Join("testdata", "slot.golden")
	got := encodeSlot(fixedGoldenSlot())
	if os.Getenv("GOGIT_VAULT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("slot encoding drifted from golden file\ngot:  %x\nwant: %x", got, want)
	}
	decoded, n, err := decodeSlot(got)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(got) {
		t.Fatalf("consumed %d bytes, want %d", n, len(got))
	}
	want2 := fixedGoldenSlot()
	if decoded.Kind != want2.Kind || !bytes.Equal(decoded.Salt, want2.Salt) ||
		!bytes.Equal(decoded.Nonce, want2.Nonce) || !bytes.Equal(decoded.Wrapped, want2.Wrapped) ||
		decoded.Params != want2.Params {
		t.Fatalf("decoded slot mismatch: %+v want %+v", decoded, want2)
	}
}

func TestEncodeDecodeSlotRoundTrip(t *testing.T) {
	cases := []Slot{
		{Kind: SlotPassword, Salt: []byte("s"), Nonce: []byte("n"), Wrapped: []byte("w"), Params: SlotParams{Time: 1, Memory: 8192, Threads: 1}},
		{Kind: SlotFile, Salt: nil, Nonce: []byte{1, 2, 3}, Wrapped: []byte{4, 5, 6, 7}, Params: SlotParams{}},
	}
	for _, c := range cases {
		buf := encodeSlot(c)
		decoded, n, err := decodeSlot(buf)
		if err != nil {
			t.Fatalf("decodeSlot: %v", err)
		}
		if n != len(buf) {
			t.Fatalf("consumed %d, want %d", n, len(buf))
		}
		if decoded.Kind != c.Kind || !bytes.Equal(decoded.Salt, c.Salt) ||
			!bytes.Equal(decoded.Nonce, c.Nonce) || !bytes.Equal(decoded.Wrapped, c.Wrapped) ||
			decoded.Params != c.Params {
			t.Fatalf("round trip mismatch: got %+v want %+v", decoded, c)
		}
	}
}

func TestDecodeSlotRejectsTruncation(t *testing.T) {
	full := encodeSlot(fixedGoldenSlot())
	for n := 0; n < len(full); n++ {
		if _, _, err := decodeSlot(full[:n]); err == nil {
			t.Fatalf("truncation at %d/%d did not error", n, len(full))
		}
	}
}

func TestEncodeHeaderRejectsSlotCount(t *testing.T) {
	if _, err := encodeHeader(formatVersion, 0, nil); !errors.Is(err, ErrSlotCount) {
		t.Fatalf("empty slots: got %v, want ErrSlotCount", err)
	}
	tooMany := make([]Slot, maxSlotCount+1)
	for i := range tooMany {
		tooMany[i] = fixedGoldenSlot()
	}
	if _, err := encodeHeader(formatVersion, 0, tooMany); !errors.Is(err, ErrSlotCount) {
		t.Fatalf("too many slots: got %v, want ErrSlotCount", err)
	}
}

func TestDecodeHeaderRejectsBadMagic(t *testing.T) {
	data, err := encodeHeader(formatVersion, 0, []Slot{fixedGoldenSlot()})
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 0xFF
	if _, _, _, _, err := decodeHeader(data); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("got %v, want ErrInvalidFormat", err)
	}
}

func TestDecodeHeaderRejectsShort(t *testing.T) {
	if _, _, _, _, err := decodeHeader([]byte("short")); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("got %v, want ErrInvalidFormat", err)
	}
}

func TestDecodeHeaderRejectsBadVersion(t *testing.T) {
	data, err := encodeHeader(formatVersion, 0, []Slot{fixedGoldenSlot()})
	if err != nil {
		t.Fatal(err)
	}
	data[8] = 0xFF
	if _, _, _, _, err := decodeHeader(data); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}
}

func TestDecodeHeaderRejectsBadSlotCount(t *testing.T) {
	slot := fixedGoldenSlot()
	for _, count := range []byte{0, maxSlotCount + 1, 255} {
		data, err := encodeHeader(formatVersion, 0, []Slot{slot})
		if err != nil {
			t.Fatal(err)
		}
		data[12] = count
		if _, _, _, _, err := decodeHeader(data); !errors.Is(err, ErrInvalidFormat) {
			t.Fatalf("count %d: got %v, want ErrInvalidFormat", count, err)
		}
	}
}

func TestDecodeHeaderRejectsTruncatedSlots(t *testing.T) {
	data, err := encodeHeader(formatVersion, 0, []Slot{fixedGoldenSlot()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := decodeHeader(data[:len(data)-1]); err == nil {
		t.Fatal("expected error for truncated slot data")
	}
}

func TestHeaderEveryByteCorruptionFails(t *testing.T) {
	slot := fixedGoldenSlot()
	headerBytes, err := encodeHeader(formatVersion, 0, []Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, payloadNonceSize)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	ciphertext := make([]byte, 40)
	for i := range ciphertext {
		ciphertext[i] = byte(255 - i)
	}
	original := append(append(append([]byte{}, headerBytes...), nonce...), ciphertext...)

	for i := 0; i < len(headerBytes); i++ {
		corrupt := append([]byte(nil), original...)
		corrupt[i] ^= 0xFF
		version, flags, slots, n, hErr := decodeHeader(corrupt)
		if hErr != nil {
			continue
		}
		rest := corrupt[n:]
		if len(rest) <= payloadNonceSize {
			continue
		}
		gotNonce := rest[:payloadNonceSize]
		gotCipher := rest[payloadNonceSize:]
		if version == formatVersion && flags == 0 && len(slots) == 1 &&
			bytes.Equal(gotNonce, nonce) && bytes.Equal(gotCipher, ciphertext) &&
			slots[0].Kind == slot.Kind && bytes.Equal(slots[0].Salt, slot.Salt) &&
			bytes.Equal(slots[0].Nonce, slot.Nonce) && bytes.Equal(slots[0].Wrapped, slot.Wrapped) &&
			slots[0].Params == slot.Params {
			t.Fatalf("byte %d corruption was not observable in decoded header", i)
		}
	}
}

func TestReadVaultFileMissing(t *testing.T) {
	_, _, _, _, _, err := readVaultFile(filepath.Join(t.TempDir(), "missing.bin"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadVaultFileTooShortPayload(t *testing.T) {
	headerBytes, err := encodeHeader(formatVersion, 0, []Slot{fixedGoldenSlot()})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "vault.bin")
	if err := os.WriteFile(path, append(headerBytes, make([]byte, payloadNonceSize)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := readVaultFile(path); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("got %v, want ErrInvalidFormat", err)
	}
}

func TestReadVaultFileInvalidHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	if err := os.WriteFile(path, []byte("not a vault file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := readVaultFile(path); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("got %v, want ErrInvalidFormat", err)
	}
}

func TestWriteAtomicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "vault.bin")
	if err := writeAtomic(path, []byte("data")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp to be gone, stat err = %v", err)
	}
}

func TestWriteAtomicFailsWhenDirIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(blocker, "vault.bin"), []byte("x")); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteAtomicFailsWhenTargetIsDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vault.bin")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("x")); err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteAtomicFailsWhenTempIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "vault.bin.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(dir, "vault.bin"), []byte("x")); err == nil {
		t.Fatal("expected error")
	}
}
