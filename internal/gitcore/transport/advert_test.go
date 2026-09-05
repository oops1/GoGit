package transport

import (
	"bytes"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func idOf(seed byte) hash.ObjectID {
	var raw [hash.Size]byte
	raw[hash.Size-1] = seed
	id, err := hash.FromBytes(raw[:])
	if err != nil {
		panic(err)
	}
	return id
}

func writePktString(t *testing.T, enc *Encoder, text string) {
	t.Helper()
	if err := enc.WriteData([]byte(text)); err != nil {
		t.Fatalf("WriteData returned error %v", err)
	}
}

func buildV1Advertisement(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	head := idOf(1).String() + " HEAD\x00multi_ack_detailed side-band-64k ofs-delta symref=HEAD:refs/heads/main agent=gogit/test\n"
	writePktString(t, enc, head)
	writePktString(t, enc, idOf(1).String()+" refs/heads/main\n")
	writePktString(t, enc, idOf(2).String()+" refs/heads/other\n")
	writePktString(t, enc, idOf(3).String()+" refs/tags/v1\n")
	writePktString(t, enc, idOf(4).String()+" refs/tags/v1^{}\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	return buf.Bytes()
}

func buildV0Advertisement(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	head := idOf(1).String() + " HEAD\x00multi_ack thin-pack\n"
	writePktString(t, enc, head)
	writePktString(t, enc, idOf(1).String()+" refs/heads/main\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	return buf.Bytes()
}

func buildV1EmptyRepositoryAdvertisement(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	head := hash.Zero.String() + " capabilities^{}\x00report-status delete-refs side-band-64k\n"
	writePktString(t, enc, head)
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	return buf.Bytes()
}

func buildV2Advertisement(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, "version 2\n")
	writePktString(t, enc, "ls-refs\n")
	writePktString(t, enc, "fetch=shallow filter\n")
	writePktString(t, enc, "object-format=sha1\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	return buf.Bytes()
}

func buildVersion1MarkerAdvertisement(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, "version 1\n")
	head := idOf(1).String() + " HEAD\x00symref=HEAD:refs/heads/main\n"
	writePktString(t, enc, head)
	writePktString(t, enc, idOf(1).String()+" refs/heads/main\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	return buf.Bytes()
}

func TestParseAdvertisementV1(t *testing.T) {
	adv, err := ParseAdvertisement(bytes.NewReader(buildV1Advertisement(t)))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if adv.Version != 0 {
		t.Errorf("Version = %d, want 0", adv.Version)
	}
	if len(adv.Refs) != 4 {
		t.Fatalf("Refs = %v, want 4 entries", adv.Refs)
	}
	if adv.Refs[0].Name != "HEAD" || adv.Refs[0].ID != idOf(1) {
		t.Errorf("Refs[0] = %+v", adv.Refs[0])
	}
	if adv.Refs[0].Symref != "refs/heads/main" {
		t.Errorf("Refs[0].Symref = %q, want %q", adv.Refs[0].Symref, "refs/heads/main")
	}
	if adv.Head != "refs/heads/main" {
		t.Errorf("Head = %q, want %q", adv.Head, "refs/heads/main")
	}
	tag := adv.Refs[3]
	if tag.Name != "refs/tags/v1" || tag.ID != idOf(3) || tag.Peeled != idOf(4) {
		t.Errorf("Refs[3] = %+v, want peeled tag", tag)
	}
	if !adv.Capabilities.Has(CapOfsDelta) {
		t.Errorf("Capabilities missing %q", CapOfsDelta)
	}
	if value, ok := adv.Capabilities.Value(CapAgent); !ok || value != "gogit/test" {
		t.Errorf("Capabilities agent = (%q, %v)", value, ok)
	}
}

func TestParseAdvertisementV0HasNoVersionMarker(t *testing.T) {
	adv, err := ParseAdvertisement(bytes.NewReader(buildV0Advertisement(t)))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if adv.Version != 0 {
		t.Errorf("Version = %d, want 0", adv.Version)
	}
	if len(adv.Refs) != 2 || adv.Refs[0].Name != "HEAD" || adv.Refs[1].Name != "refs/heads/main" {
		t.Fatalf("Refs = %v", adv.Refs)
	}
}

func TestParseAdvertisementVersion1Marker(t *testing.T) {
	adv, err := ParseAdvertisement(bytes.NewReader(buildVersion1MarkerAdvertisement(t)))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if adv.Version != 1 {
		t.Errorf("Version = %d, want 1", adv.Version)
	}
	if adv.Head != "refs/heads/main" {
		t.Errorf("Head = %q, want %q", adv.Head, "refs/heads/main")
	}
}

func TestParseAdvertisementEmptyRepositoryIsNotAnError(t *testing.T) {
	adv, err := ParseAdvertisement(bytes.NewReader(buildV1EmptyRepositoryAdvertisement(t)))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v, want nil for an empty repository", err)
	}
	if len(adv.Refs) != 0 {
		t.Fatalf("Refs = %v, want none for an empty repository", adv.Refs)
	}
	if !adv.Capabilities.Has(CapDeleteRefs) {
		t.Fatalf("Capabilities missing %q", CapDeleteRefs)
	}
}

func TestParseAdvertisementV2(t *testing.T) {
	adv, err := ParseAdvertisement(bytes.NewReader(buildV2Advertisement(t)))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if adv.Version != 2 {
		t.Errorf("Version = %d, want 2", adv.Version)
	}
	if len(adv.Refs) != 0 {
		t.Errorf("Refs = %v, want none in a v2 advertisement", adv.Refs)
	}
	if !adv.Capabilities.Has("ls-refs") {
		t.Errorf("Capabilities missing ls-refs")
	}
	if value, ok := adv.Capabilities.Value("fetch"); !ok || value != "shallow filter" {
		t.Errorf("Capabilities fetch = (%q, %v)", value, ok)
	}
}

func TestParseAdvertisementShallow(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, idOf(1).String()+" HEAD\x00shallow\n")
	writePktString(t, enc, idOf(1).String()+" refs/heads/main\n")
	writePktString(t, enc, "shallow "+idOf(9).String()+"\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	adv, err := ParseAdvertisement(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if len(adv.Shallow) != 1 || adv.Shallow[0] != idOf(9) {
		t.Fatalf("Shallow = %v", adv.Shallow)
	}
}

func TestParseAdvertisementErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{"empty-stream", nil},
		{"missing-nul-separator", pktLines(t, idOf(1).String()+" HEAD\n")},
		{"missing-object-id", pktLines(t, idOf(1).String()+"HEAD\x00\n")},
		{"bad-object-id", pktLines(t, "zz HEAD\x00\n", "shallow\n")},
		{"line-without-object-id", pktLines(t, idOf(1).String()+" HEAD\x00\n", "onlyname\n")},
		{"bad-shallow-id", pktLines(t, idOf(1).String()+" HEAD\x00\n", "shallow zz\n")},
		{"unmatched-peel", pktLines(t, idOf(1).String()+" HEAD\x00\n", idOf(2).String()+" refs/tags/v1^{}\n")},
		{"bad-peel-id", pktLines(t, idOf(1).String()+" HEAD\x00\n", "zz refs/tags/v1^{}\n")},
		{"missing-flush", pktLinesNoFlush(t, idOf(1).String()+" HEAD\x00\n")},
		{"missing-refs-after-version1", pktLinesNoFlush(t, "version 1\n")},
		{"bad-symref", pktLines(t, idOf(1).String()+" HEAD\x00symref=HEAD\n")},
		{"missing-flush-v2", pktLinesNoFlush(t, "version 2\n", "ls-refs\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseAdvertisement(bytes.NewReader(test.raw))
			if err == nil {
				t.Fatalf("ParseAdvertisement(%q) returned no error", test.raw)
			}
		})
	}
}

func TestParseAdvertisementUnmatchedPeelReportsError(t *testing.T) {
	raw := pktLines(t, idOf(1).String()+" HEAD\x00\n", idOf(2).String()+" refs/tags/v1^{}\n")
	_, err := ParseAdvertisement(bytes.NewReader(raw))
	if !errors.Is(err, ErrAdvertisementMalformed) {
		t.Fatalf("ParseAdvertisement returned %v, want ErrAdvertisementMalformed", err)
	}
}

func TestParseAdvertisementDecodeErrorPropagates(t *testing.T) {
	_, err := ParseAdvertisement(bytes.NewReader([]byte("zzzz")))
	if !errors.Is(err, ErrPktLineBadLength) {
		t.Fatalf("ParseAdvertisement returned %v, want ErrPktLineBadLength", err)
	}
}

func TestParseAdvertisementDecodeErrorAfterFirstLine(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, idOf(1).String()+" HEAD\x00\n")
	buf.WriteString("zzzz")
	_, err := ParseAdvertisement(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrPktLineBadLength) {
		t.Fatalf("ParseAdvertisement returned %v, want ErrPktLineBadLength", err)
	}
}

func TestParseAdvertisementDecodeErrorAfterVersion2(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, "version 2\n")
	buf.WriteString("zzzz")
	_, err := ParseAdvertisement(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrPktLineBadLength) {
		t.Fatalf("ParseAdvertisement returned %v, want ErrPktLineBadLength", err)
	}
}

func TestParseAdvertisementDecodeErrorAfterVersion1Marker(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, "version 1\n")
	buf.WriteString("zzzz")
	_, err := ParseAdvertisement(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrPktLineBadLength) {
		t.Fatalf("ParseAdvertisement returned %v, want ErrPktLineBadLength", err)
	}
}

func TestParseAdvertisementSkipsNonDataPacketsInV1Refs(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, idOf(1).String()+" HEAD\x00\n")
	if err := enc.WriteDelim(); err != nil {
		t.Fatalf("WriteDelim returned error %v", err)
	}
	writePktString(t, enc, idOf(2).String()+" refs/heads/other\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	adv, err := ParseAdvertisement(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if len(adv.Refs) != 2 || adv.Refs[1].Name != "refs/heads/other" {
		t.Fatalf("Refs = %v", adv.Refs)
	}
}

func TestParseAdvertisementBadObjectIDOnSubsequentLine(t *testing.T) {
	raw := pktLines(t, idOf(1).String()+" HEAD\x00\n", "zz refs/heads/other\n")
	_, err := ParseAdvertisement(bytes.NewReader(raw))
	if !errors.Is(err, ErrAdvertisementMalformed) {
		t.Fatalf("ParseAdvertisement returned %v, want ErrAdvertisementMalformed", err)
	}
}

func TestParseAdvertisementSkipsNonDataPacketsInV2Capabilities(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	writePktString(t, enc, "version 2\n")
	if err := enc.WriteDelim(); err != nil {
		t.Fatalf("WriteDelim returned error %v", err)
	}
	writePktString(t, enc, "ls-refs\n")
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	adv, err := ParseAdvertisement(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ParseAdvertisement returned error %v", err)
	}
	if !adv.Capabilities.Has("ls-refs") {
		t.Fatalf("Capabilities = %v, want ls-refs", adv.Capabilities.Names())
	}
}

func pktLines(t *testing.T, lines ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	for _, line := range lines {
		writePktString(t, enc, line)
	}
	if err := enc.WriteFlush(); err != nil {
		t.Fatalf("WriteFlush returned error %v", err)
	}
	return buf.Bytes()
}

func pktLinesNoFlush(t *testing.T, lines ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	for _, line := range lines {
		writePktString(t, enc, line)
	}
	return buf.Bytes()
}
