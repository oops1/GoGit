package credential

import (
	"bytes"
	"testing"
)

func TestUTF16BlobRoundTripsTextOutsideTheBasicPlane(t *testing.T) {
	text := []byte("пароль-😀-ok")
	blob := utf16LEFromUTF8(text)
	if len(blob) != 2*len([]rune(string(text)))+2 {
		t.Fatalf("blob length = %d", len(blob))
	}
	if got := utf8FromUTF16LE(blob); !bytes.Equal(got, text) {
		t.Fatalf("round trip = %q, want %q", got, text)
	}
}

func TestUTF16BlobDropsAnOddTrailingByte(t *testing.T) {
	blob := append(utf16LEFromUTF8([]byte("ab")), 0x41)
	if got := utf8FromUTF16LE(blob); string(got) != "ab" {
		t.Fatalf("decoded = %q, want ab", got)
	}
}

func TestSecretsEqualComparesWholeValues(t *testing.T) {
	if !secretsEqual([]byte("pw"), []byte("pw")) || secretsEqual([]byte("pw"), []byte("pw2")) {
		t.Fatal("secretsEqual must compare the whole secret")
	}
}
