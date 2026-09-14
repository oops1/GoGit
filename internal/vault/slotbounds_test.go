package vault

import (
	"errors"
	"testing"
)

func TestDecodeSlotRejectsPasswordParametersOutOfBounds(t *testing.T) {
	for _, params := range []SlotParams{
		{Time: 0, Memory: 8192, Threads: 1},
		{Time: 11, Memory: 8192, Threads: 1},
		{Time: 64, Memory: 8192, Threads: 1},
		{Time: 1, Memory: 1024, Threads: 1},
		{Time: 1, Memory: 1024*1024 + 1, Threads: 1},
		{Time: 1, Memory: 4 * 1024 * 1024, Threads: 1},
		{Time: 1, Memory: 8192, Threads: 0},
		{Time: 1, Memory: 8192, Threads: 17},
	} {
		slot := Slot{Kind: SlotPassword, Salt: []byte("s"), Nonce: []byte("n"), Wrapped: []byte("w"), Params: params}
		if _, _, err := decodeSlot(encodeSlot(slot)); !errors.Is(err, ErrInvalidFormat) {
			t.Fatalf("params %+v: err = %v, want ErrInvalidFormat", params, err)
		}
	}
}

func TestDecodeSlotAcceptsPasswordParametersAtTheBounds(t *testing.T) {
	for _, params := range []SlotParams{
		DefaultSlotParams(),
		TestSlotParams(),
		{Time: 10, Memory: 1024 * 1024, Threads: 16},
	} {
		slot := Slot{Kind: SlotPassword, Salt: []byte("s"), Nonce: []byte("n"), Wrapped: []byte("w"), Params: params}
		if _, _, err := decodeSlot(encodeSlot(slot)); err != nil {
			t.Fatalf("params %+v: err = %v", params, err)
		}
	}
}
