package style

import (
	"testing"

	"github.com/oops1/headless-gui/v4/engine"
)

func TestTextIsHintedOnlyVerticallyWithRaisedContrast(t *testing.T) {
	got := TextQuality()
	if !got.Hinting || got.HintingX || got.LCD || got.Contrast != engine.DefaultTextContrast {
		t.Fatalf("text quality = %+v, want vertical hinting and default contrast only", got)
	}
}
