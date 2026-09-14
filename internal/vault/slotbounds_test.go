package vault

import (
	"errors"
	"testing"
)

func TestDecodeSlotRejectsPasswordParametersOutOfBounds(t *testing.T) {
	for _, params := range []SlotParams{
		{Time: 0, Memory: 8192, Threads: 1},
		{Time: 65, Memory: 8192, Threads: 1},
		{Time: 1, Memory: 1024, Threads: 1},
		{Time: 1, Memory: 5 * 1024 * 1024, Threads: 1},
		{Time: 1, Memory: 8192, Threads: 0},
	} {
		slot := Slot{Kind: SlotPassword, Salt: []byte("s"), Nonce: []byte("n"), Wrapped: []byte("w"), Params: params}
		if _, _, err := decodeSlot(encodeSlot(slot)); !errors.Is(err, ErrInvalidFormat) {
			t.Fatalf("params %+v: err = %v, want ErrInvalidFormat", params, err)
		}
	}
}
