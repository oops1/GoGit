package settings

import (
	"errors"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/svg"
)

func TestBuildLockIconReturnsNilWhenParseFails(t *testing.T) {
	prev := parseLockIconSVG
	parseLockIconSVG = func([]byte) (*svg.Document, error) { return nil, errors.New("boom") }
	defer func() { parseLockIconSVG = prev }()

	if buildLockIcon(color.RGBA{}) != nil {
		t.Fatal("expected nil icon when the SVG fails to parse")
	}
}

func TestBuildLockIconRendersASquareImage(t *testing.T) {
	img := buildLockIcon(color.RGBA{R: 128, G: 128, B: 128, A: 255})
	if img == nil {
		t.Fatal("expected a non-nil icon")
	}
	b := img.Bounds()
	if b.Dx() != lockIconSize || b.Dy() != lockIconSize {
		t.Fatalf("icon bounds = %v, want %dx%d", b, lockIconSize, lockIconSize)
	}
}
