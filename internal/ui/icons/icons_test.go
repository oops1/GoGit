package icons

import (
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/svg"
)

func TestStatusRendersKnownIconAtRequestedSize(t *testing.T) {
	resetForTest()
	img := Status("added", 16)
	if img == nil {
		t.Fatal("Status(added, 16) = nil")
	}
	if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("bounds = %v, want 16x16", b)
	}
}

func TestStatusCachesRasterizedImageByNameAndSize(t *testing.T) {
	resetForTest()
	first := Status("modified", 20)
	second := Status("modified", 20)
	if first == nil || second == nil {
		t.Fatal("expected non-nil images")
	}
	if first != second {
		t.Fatal("expected cached image to be returned for the same name and size")
	}
	other := Status("modified", 24)
	if other == first {
		t.Fatal("expected a different image for a different size")
	}
}

func TestStatusReturnsNilForUnknownName(t *testing.T) {
	resetForTest()
	if img := Status("does-not-exist", 16); img != nil {
		t.Fatalf("Status(does-not-exist, 16) = %v, want nil", img)
	}
	if img := Status("does-not-exist", 16); img != nil {
		t.Fatalf("second call: Status(does-not-exist, 16) = %v, want nil", img)
	}
}

func TestStatusReturnsNilForNonPositiveSize(t *testing.T) {
	resetForTest()
	if img := Status("added", 0); img != nil {
		t.Fatalf("Status(added, 0) = %v, want nil", img)
	}
	if img := Status("added", -1); img != nil {
		t.Fatalf("Status(added, -1) = %v, want nil", img)
	}
}

func TestStatusTintedRendersKnownIconAtRequestedSize(t *testing.T) {
	resetForTest()
	img := StatusTinted("added", 16, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	if img == nil {
		t.Fatal("StatusTinted(added, 16, gray) = nil")
	}
	if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("bounds = %v, want 16x16", b)
	}
}

func TestStatusTintedReturnsNilForUnknownName(t *testing.T) {
	resetForTest()
	if img := StatusTinted("does-not-exist", 16, color.RGBA{}); img != nil {
		t.Fatalf("StatusTinted(does-not-exist, 16, _) = %v, want nil", img)
	}
}

func TestStatusTintedReturnsNilForNonPositiveSize(t *testing.T) {
	resetForTest()
	if img := StatusTinted("added", 0, color.RGBA{}); img != nil {
		t.Fatalf("StatusTinted(added, 0, _) = %v, want nil", img)
	}
}

func TestStatusTintedDiffersFromThePlainStatusIcon(t *testing.T) {
	resetForTest()
	plain := Status("added", 16)
	tinted := StatusTinted("added", 16, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	if plain == nil || tinted == nil {
		t.Fatal("expected non-nil images")
	}
	if sameImage(plain, tinted) {
		t.Fatal("expected the tinted icon to differ from the plain icon")
	}
}

func TestTreeRendersKnownIconAtRequestedSize(t *testing.T) {
	resetForTest()
	img := Tree("branch", 20)
	if img == nil {
		t.Fatal("Tree(branch, 20) = nil")
	}
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 20 {
		t.Fatalf("bounds = %v, want 20x20", b)
	}
}

func TestTreeReturnsNilForUnknownName(t *testing.T) {
	resetForTest()
	if img := Tree("does-not-exist", 16); img != nil {
		t.Fatalf("Tree(does-not-exist, 16) = %v, want nil", img)
	}
}

func TestDocumentParseFailureIsCachedAsNil(t *testing.T) {
	resetForTest()
	original := parseSVG
	calls := 0
	parseSVG = func(data []byte) (*svg.Document, error) {
		calls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { parseSVG = original })

	if img := Status("added", 16); img != nil {
		t.Fatalf("Status(added, 16) = %v, want nil", img)
	}
	if img := Status("added", 16); img != nil {
		t.Fatalf("second call: Status(added, 16) = %v, want nil", img)
	}
	if calls != 1 {
		t.Fatalf("parseSVG called %d times, want 1 (failure must be cached)", calls)
	}
}

func TestToolbarRendersKnownIconAtRequestedSize(t *testing.T) {
	resetForTest()
	img := Toolbar("pull", 16, color.RGBA{R: 255, A: 255})
	if img == nil {
		t.Fatal("Toolbar(pull, 16, red) = nil")
	}
	if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("bounds = %v, want 16x16", b)
	}
}

func TestToolbarReturnsNilForUnknownName(t *testing.T) {
	resetForTest()
	if img := Toolbar("does-not-exist", 16, color.RGBA{}); img != nil {
		t.Fatalf("Toolbar(does-not-exist, 16, _) = %v, want nil", img)
	}
	if img := Toolbar("does-not-exist", 16, color.RGBA{}); img != nil {
		t.Fatalf("second call: Toolbar(does-not-exist, 16, _) = %v, want nil", img)
	}
}

func TestToolbarReturnsNilForNonPositiveSize(t *testing.T) {
	resetForTest()
	if img := Toolbar("pull", 0, color.RGBA{}); img != nil {
		t.Fatalf("Toolbar(pull, 0, _) = %v, want nil", img)
	}
	if img := Toolbar("pull", -1, color.RGBA{}); img != nil {
		t.Fatalf("Toolbar(pull, -1, _) = %v, want nil", img)
	}
}

func TestToolbarRecolorsTheIconWithTheGivenTint(t *testing.T) {
	resetForTest()
	red := Toolbar("pull", 16, color.RGBA{R: 255, A: 255})
	blue := Toolbar("pull", 16, color.RGBA{B: 255, A: 255})
	if red == nil || blue == nil {
		t.Fatal("expected non-nil images")
	}
	if sameImage(red, blue) {
		t.Fatal("expected a different rasterization for a different tint")
	}
}

func TestToolbarCachesRasterizedImageByNameSizeAndTint(t *testing.T) {
	resetForTest()
	tint := color.RGBA{G: 255, A: 255}
	first := Toolbar("push", 16, tint)
	second := Toolbar("push", 16, tint)
	if first == nil || second == nil {
		t.Fatal("expected non-nil images")
	}
	if first != second {
		t.Fatal("expected cached image to be returned for the same name, size and tint")
	}
}

func TestTreeTintedRendersKnownIconAtRequestedSize(t *testing.T) {
	resetForTest()
	img := TreeTinted("repository", 16, color.RGBA{R: 255, A: 255})
	if img == nil {
		t.Fatal("TreeTinted(repository, 16, red) = nil")
	}
	if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("bounds = %v, want 16x16", b)
	}
}

func TestTreeTintedReturnsNilForUnknownName(t *testing.T) {
	resetForTest()
	if img := TreeTinted("does-not-exist", 16, color.RGBA{}); img != nil {
		t.Fatalf("TreeTinted(does-not-exist, 16, _) = %v, want nil", img)
	}
}

func TestTreeTintedReturnsNilForNonPositiveSize(t *testing.T) {
	resetForTest()
	if img := TreeTinted("repository", 0, color.RGBA{}); img != nil {
		t.Fatalf("TreeTinted(repository, 0, _) = %v, want nil", img)
	}
}

func TestTreeTintedDiffersFromThePlainTreeIcon(t *testing.T) {
	resetForTest()
	plain := Tree("repository", 16)
	tinted := TreeTinted("repository", 16, color.RGBA{R: 76, G: 194, B: 255, A: 255})
	if plain == nil || tinted == nil {
		t.Fatal("expected non-nil images")
	}
	if sameImage(plain, tinted) {
		t.Fatal("expected the accent-tinted icon to differ from the plain icon")
	}
}

func sameImage(a, b image.Image) bool {
	ba, bb := a.Bounds(), b.Bounds()
	if ba != bb {
		return false
	}
	for y := ba.Min.Y; y < ba.Max.Y; y++ {
		for x := ba.Min.X; x < ba.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}

func TestConcurrentAccessIsSafe(t *testing.T) {
	resetForTest()
	names := []string{"added", "modified", "conflict", "does-not-exist"}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Go(func() {
			for _, name := range names {
				Status(name, 16)
				StatusTinted(name, 16, color.RGBA{R: 128, G: 128, B: 128, A: 255})
				Tree("branch", 16)
				TreeTinted("branch", 16, color.RGBA{R: 76, G: 194, B: 255, A: 255})
				Toolbar("pull", 16, color.RGBA{R: 255, A: 255})
			}
		})
	}
	wg.Wait()
}

func TestStatusMutedKeepsTheGlyphButDropsColourAndOpacity(t *testing.T) {
	full := Status("modified", 16)
	dim := StatusMuted("modified", 16)
	if full == nil || dim == nil {
		t.Fatal("both renders must produce an image")
	}
	if sameImage(full, dim) {
		t.Fatal("the muted icon must differ from the full colour one")
	}
	var opaque, coloured int
	b := dim.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := dim.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			opaque++
			if r != g || g != bl {
				coloured++
			}
		}
	}
	if opaque == 0 {
		t.Fatal("the muted icon lost every pixel")
	}
	if coloured != 0 {
		t.Fatalf("%d muted pixels kept their colour", coloured)
	}
}

func TestStatusMutedReturnsNothingForAnUnknownIcon(t *testing.T) {
	if StatusMuted("nope", 16) != nil {
		t.Fatal("an unknown icon must not render")
	}
	if StatusMuted("modified", 0) != nil {
		t.Fatal("a zero size must not render")
	}
}

func TestToolbarPlainRendersTheIconWithItsOwnColours(t *testing.T) {
	plain := ToolbarPlain("pull", 24)
	if plain == nil {
		t.Fatal("the toolbar icon must render")
	}
	tinted := Toolbar("pull", 24, color.RGBA{R: 255, A: 255})
	if sameImage(plain, tinted) {
		t.Fatal("a plain render must differ from a tinted one")
	}
	if ToolbarPlain("nope", 24) != nil {
		t.Fatal("an unknown toolbar icon must not render")
	}
}

func TestToolbarMutedGreysTheIcon(t *testing.T) {
	plain := ToolbarPlain("subdirs", 16)
	dim := ToolbarMuted("subdirs", 16)
	if plain == nil || dim == nil {
		t.Fatal("both renders must produce an image")
	}
	if sameImage(plain, dim) {
		t.Fatal("the muted toolbar icon must differ from the full colour one")
	}
	if ToolbarMuted("nope", 16) != nil {
		t.Fatal("an unknown icon must not render")
	}
}
