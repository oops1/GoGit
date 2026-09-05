package transport

import (
	"bytes"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func mustWritePkt(enc *Encoder, text string) {
	if err := enc.WriteData([]byte(text)); err != nil {
		panic(err)
	}
}

func fuzzSeedV1Advertisement() []byte {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	mustWritePkt(enc, idOf(1).String()+" HEAD\x00multi_ack_detailed side-band-64k ofs-delta symref=HEAD:refs/heads/main agent=gogit/test\n")
	mustWritePkt(enc, idOf(1).String()+" refs/heads/main\n")
	mustWritePkt(enc, idOf(2).String()+" refs/heads/other\n")
	mustWritePkt(enc, idOf(3).String()+" refs/tags/v1\n")
	mustWritePkt(enc, idOf(4).String()+" refs/tags/v1^{}\n")
	if err := enc.WriteFlush(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func fuzzSeedV2Advertisement() []byte {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	mustWritePkt(enc, "version 2\n")
	mustWritePkt(enc, "ls-refs\n")
	mustWritePkt(enc, "fetch=shallow filter\n")
	if err := enc.WriteFlush(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func fuzzSeedEmptyRepositoryAdvertisement() []byte {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	mustWritePkt(enc, hash.Zero.String()+" capabilities^{}\x00report-status delete-refs side-band-64k\n")
	if err := enc.WriteFlush(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func FuzzDecodePktLine(f *testing.F) {
	seeds := [][]byte{
		nil,
		[]byte("0000"),
		[]byte("0001"),
		[]byte("0002"),
		[]byte("0003"),
		[]byte("0006ab"),
		[]byte("fff0"),
		[]byte("zzzz"),
		[]byte("0009hel"),
		fuzzSeedV1Advertisement(),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dec := NewDecoder(bytes.NewReader(data))
		packets := 0
		for dec.Scan() {
			packets++
			_ = dec.Type().String()
			if len(dec.Bytes()) > MaxDataLength {
				t.Fatalf("Bytes() returned %d bytes, exceeding MaxDataLength", len(dec.Bytes()))
			}
			if packets > 4096 {
				break
			}
		}
		_ = dec.Err()
	})
}

func FuzzParseAdvertisement(f *testing.F) {
	f.Add(fuzzSeedV1Advertisement())
	f.Add(fuzzSeedV2Advertisement())
	f.Add(fuzzSeedEmptyRepositoryAdvertisement())
	f.Add([]byte{})
	f.Add([]byte("0000"))
	f.Fuzz(func(t *testing.T, data []byte) {
		adv, err := ParseAdvertisement(bytes.NewReader(data))
		if err != nil {
			return
		}
		if adv.Version < 0 || adv.Version > 2 {
			t.Fatalf("Advertisement.Version = %d, want 0, 1 or 2", adv.Version)
		}
		for _, ref := range adv.Refs {
			_ = ref.Name
		}
		for _, value := range adv.Capabilities.Names() {
			_ = value
		}
	})
}
