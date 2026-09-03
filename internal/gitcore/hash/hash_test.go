package hash_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	helloBlobID = "ce013625030ba8dba906f756967f9e9ca394464a"
	emptyBlobID = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	emptyTreeID = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
)

func mustParse(t *testing.T, text string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q) = %v", text, err)
	}
	return id
}

func TestParseAcceptsFullHexAndPrintsItBack(t *testing.T) {
	id := mustParse(t, helloBlobID)
	if id.String() != helloBlobID {
		t.Fatalf("String() = %q, want %q", id.String(), helloBlobID)
	}
}

func TestParseNormalizesUppercaseHexToLowercase(t *testing.T) {
	id, err := hash.Parse(strings.ToUpper(helloBlobID))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if id.String() != helloBlobID {
		t.Fatalf("String() = %q, want %q", id.String(), helloBlobID)
	}
}

func TestParseRejectsWrongLength(t *testing.T) {
	for _, text := range []string{"", "ab", helloBlobID + "0", helloBlobID[:39]} {
		if _, err := hash.Parse(text); !errors.Is(err, hash.ErrInvalidLength) {
			t.Fatalf("Parse(%q) err = %v, want ErrInvalidLength", text, err)
		}
	}
}

func TestParseRejectsNonHexDigits(t *testing.T) {
	if _, err := hash.Parse(strings.Repeat("z", hash.HexSize)); !errors.Is(err, hash.ErrInvalidHex) {
		t.Fatalf("err = %v, want ErrInvalidHex", err)
	}
}

func TestFromBytesAcceptsExactlyTwentyBytes(t *testing.T) {
	want := mustParse(t, helloBlobID)
	got, err := hash.FromBytes(want.Bytes())
	if err != nil || got != want {
		t.Fatalf("FromBytes = %v, %v; want %v, nil", got, err, want)
	}
}

func TestFromBytesRejectsWrongLength(t *testing.T) {
	if _, err := hash.FromBytes(make([]byte, hash.Size-1)); !errors.Is(err, hash.ErrInvalidLength) {
		t.Fatalf("err = %v, want ErrInvalidLength", err)
	}
}

func TestIsZeroDistinguishesTheNullObjectID(t *testing.T) {
	if !hash.Zero.IsZero() {
		t.Fatal("Zero.IsZero() = false")
	}
	if mustParse(t, helloBlobID).IsZero() {
		t.Fatal("real id reported as zero")
	}
	if hash.Zero.String() != strings.Repeat("0", hash.HexSize) {
		t.Fatalf("Zero.String() = %q", hash.Zero.String())
	}
}

func TestBytesReturnsIndependentCopy(t *testing.T) {
	id := mustParse(t, helloBlobID)
	raw := id.Bytes()
	raw[0] ^= 0xff
	if id.String() != helloBlobID {
		t.Fatal("mutating Bytes() changed the object id")
	}
}

func TestCompareOrdersObjectIDsBytewise(t *testing.T) {
	low := mustParse(t, emptyTreeID)
	high := mustParse(t, helloBlobID)
	same := mustParse(t, emptyTreeID)
	if low.Compare(high) >= 0 || high.Compare(low) <= 0 || low.Compare(same) != 0 {
		t.Fatalf("Compare is not a bytewise ordering: %d %d %d",
			low.Compare(high), high.Compare(low), low.Compare(same))
	}
	if !bytes.Equal(low.Bytes(), mustParse(t, emptyTreeID).Bytes()) {
		t.Fatal("Bytes differ for equal ids")
	}
}

func TestMarshalTextRoundTripsThroughJSON(t *testing.T) {
	type holder struct {
		ID hash.ObjectID `json:"id"`
	}
	encoded, err := json.Marshal(holder{ID: mustParse(t, helloBlobID)})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"id":"` + helloBlobID + `"}`; string(encoded) != want {
		t.Fatalf("encoded = %s, want %s", encoded, want)
	}
	var back holder
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.ID.String() != helloBlobID {
		t.Fatalf("round trip lost the id: %s", back.ID)
	}
}

func TestUnmarshalTextRejectsGarbage(t *testing.T) {
	var id hash.ObjectID
	if err := id.UnmarshalText([]byte("nope")); !errors.Is(err, hash.ErrInvalidLength) {
		t.Fatalf("err = %v, want ErrInvalidLength", err)
	}
	if !id.IsZero() {
		t.Fatal("failed UnmarshalText modified the receiver")
	}
}

func TestFormatReportsNameAndDigestSizes(t *testing.T) {
	cases := []struct {
		format  hash.Format
		name    string
		size    int
		hexSize int
		ok      bool
	}{
		{hash.SHA1, "sha1", 20, 40, true},
		{hash.SHA256, "sha256", 32, 64, false},
		{hash.Format(9), "unknown", 0, 0, false},
	}
	for _, c := range cases {
		if c.format.String() != c.name || c.format.Size() != c.size ||
			c.format.HexSize() != c.hexSize || c.format.Supported() != c.ok {
			t.Fatalf("%d: got %q/%d/%d/%v", c.format, c.format.String(),
				c.format.Size(), c.format.HexSize(), c.format.Supported())
		}
	}
}

func TestParseFormatAcceptsKnownNames(t *testing.T) {
	for name, want := range map[string]hash.Format{"sha1": hash.SHA1, "sha256": hash.SHA256} {
		got, err := hash.ParseFormat(name)
		if err != nil || got != want {
			t.Fatalf("ParseFormat(%q) = %v, %v", name, got, err)
		}
	}
}

func TestParseFormatRejectsUnknownName(t *testing.T) {
	if _, err := hash.ParseFormat("blake3"); !errors.Is(err, hash.ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestHeaderUsesTypeSpaceSizeNul(t *testing.T) {
	if got := string(hash.Header("blob", 6)); got != "blob 6\x00" {
		t.Fatalf("Header = %q", got)
	}
	if got := string(hash.Header("commit", 0)); got != "commit 0\x00" {
		t.Fatalf("Header = %q", got)
	}
}

func TestSumMatchesObjectIDsProducedByGit(t *testing.T) {
	cases := []struct {
		objectType string
		data       []byte
		want       string
	}{
		{"blob", []byte("hello\n"), helloBlobID},
		{"blob", nil, emptyBlobID},
		{"tree", nil, emptyTreeID},
	}
	for _, c := range cases {
		got, err := hash.Sum(hash.SHA1, c.objectType, c.data)
		if err != nil {
			t.Fatalf("Sum: %v", err)
		}
		if got.String() != c.want {
			t.Fatalf("Sum(%s) = %s, want %s", c.objectType, got, c.want)
		}
		if direct := hash.SumSHA1(c.objectType, c.data); direct != got {
			t.Fatalf("SumSHA1 = %s, Sum = %s", direct, got)
		}
	}
}

func TestSumRejectsUnsupportedFormat(t *testing.T) {
	if _, err := hash.Sum(hash.SHA256, "blob", nil); !errors.Is(err, hash.ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestHasherStreamsContentInChunks(t *testing.T) {
	data := bytes.Repeat([]byte("go.git"), 5000)
	hasher, err := hash.NewHasher(hash.SHA1, "blob", int64(len(data)))
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	for offset := 0; offset < len(data); offset += 777 {
		end := min(offset+777, len(data))
		written, err := hasher.Write(data[offset:end])
		if err != nil || written != end-offset {
			t.Fatalf("Write = %d, %v", written, err)
		}
	}
	if hasher.Written() != int64(len(data)) {
		t.Fatalf("Written = %d, want %d", hasher.Written(), len(data))
	}
	got, err := hasher.Sum()
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	if want := hash.SumSHA1("blob", data); got != want {
		t.Fatalf("streamed %s, buffered %s", got, want)
	}
}

func TestHasherHashesEmptyContent(t *testing.T) {
	hasher, err := hash.NewHasher(hash.SHA1, "blob", 0)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	id, err := hasher.Sum()
	if err != nil || id.String() != emptyBlobID {
		t.Fatalf("Sum = %s, %v", id, err)
	}
}

func TestNewHasherRejectsUnsupportedFormat(t *testing.T) {
	if _, err := hash.NewHasher(hash.SHA256, "blob", 0); !errors.Is(err, hash.ErrUnsupportedFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestNewHasherRejectsNegativeSize(t *testing.T) {
	if _, err := hash.NewHasher(hash.SHA1, "blob", -1); !errors.Is(err, hash.ErrNegativeSize) {
		t.Fatalf("err = %v, want ErrNegativeSize", err)
	}
}

func TestHasherWriteRejectsMoreBytesThanDeclared(t *testing.T) {
	hasher, err := hash.NewHasher(hash.SHA1, "blob", 3)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	if _, err := hasher.Write([]byte("abcd")); !errors.Is(err, hash.ErrSizeMismatch) {
		t.Fatalf("err = %v, want ErrSizeMismatch", err)
	}
	if hasher.Written() != 0 {
		t.Fatalf("Written = %d after a rejected write", hasher.Written())
	}
}

func TestHasherSumRejectsIncompleteContent(t *testing.T) {
	hasher, err := hash.NewHasher(hash.SHA1, "blob", 6)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	if _, err := hasher.Write([]byte("hel")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := hasher.Sum(); !errors.Is(err, hash.ErrSizeMismatch) {
		t.Fatalf("err = %v, want ErrSizeMismatch", err)
	}
}
