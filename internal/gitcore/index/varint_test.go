package index

import (
	"errors"
	"math"
	"testing"
)

func TestVarintSurvivesEncoding(t *testing.T) {
	values := []uint64{0, 1, 126, 127, 128, 129, 255, 256, 16511, 16512, 1 << 20, math.MaxUint32, math.MaxUint64 >> 1}
	for _, value := range values {
		data := appendVarint(nil, value)
		decoded, read, err := decodeVarint(data)
		if err != nil {
			t.Fatalf("decodeVarint(%d) returned error %v", value, err)
		}
		if decoded != value || read != len(data) {
			t.Fatalf("decodeVarint(%d) returned (%d, %d)", value, decoded, read)
		}
	}
}

func TestVarintUsesTheGitOffsetEncoding(t *testing.T) {
	cases := map[uint64]string{
		0:   "\x00",
		127: "\x7f",
		128: "\x80\x00",
		129: "\x80\x01",
	}
	for value, want := range cases {
		if got := string(appendVarint(nil, value)); got != want {
			t.Fatalf("appendVarint(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestDecodeVarintRejectsBrokenNumbers(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{name: "empty", data: nil, want: ErrTruncated},
		{name: "continuation without a follower", data: []byte{0x80}, want: ErrTruncated},
		{name: "number wider than sixty four bits", data: []byte{
			0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f,
		}, want: ErrMalformed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := decodeVarint(testCase.data)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("decodeVarint returned %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestAppendVarintKeepsTheDestination(t *testing.T) {
	got := string(appendVarint([]byte("head"), 128))
	if got != "head\x80\x00" {
		t.Fatalf("appendVarint returned %q", got)
	}
}
