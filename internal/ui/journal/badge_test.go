package journal

import (
	"image/color"
	"testing"
)

func TestAuthorColorIsDeterministicForTheSameName(t *testing.T) {
	first := authorColor("Чукалин Валерий")
	second := authorColor("Чукалин Валерий")
	if first != second {
		t.Fatalf("authorColor is not deterministic: %v vs %v", first, second)
	}
}

func TestAuthorColorDiffersForDifferentNames(t *testing.T) {
	names := []string{"Чукалин Валерий", "ann", "John Smith", "Anna Maria", "root", "ci-bot"}
	colors := map[color.RGBA]bool{}
	for _, n := range names {
		colors[authorColor(n)] = true
	}
	if len(colors) < 2 {
		t.Fatalf("expected different names to map to more than one color, got %d distinct colors", len(colors))
	}
}

func TestAuthorColorIsAlwaysFromThePalette(t *testing.T) {
	inPalette := func(c color.RGBA) bool {
		for _, p := range badgePalette {
			if p == c {
				return true
			}
		}
		return false
	}
	for _, n := range []string{"a", "bb", "ccc", "дддд", "eeeee@example.com"} {
		if !inPalette(authorColor(n)) {
			t.Fatalf("authorColor(%q) = %v not in palette", n, authorColor(n))
		}
	}
}

func TestBadgeTextIsReadableOnEveryBadgeColour(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	black := color.RGBA{A: 0xFF}
	for _, bg := range badgePalette {
		got := badgeTextColor(bg)
		if got != white && got != black {
			t.Fatalf("badgeTextColor(%v) = %v, want black or white", bg, got)
		}
		if luminance(bg) > 140 && got != black {
			t.Fatalf("badgeTextColor(%v) = white on a light badge", bg)
		}
		if luminance(bg) <= 140 && got != white {
			t.Fatalf("badgeTextColor(%v) = black on a dark badge", bg)
		}
	}
}

func TestAuthorBadgeReturnsNilForZeroSize(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	if img := AuthorBadge("ann", 0); img != nil {
		t.Fatal("expected nil badge for zero size")
	}
}

func TestAuthorBadgeReturnsNilForEmptyAuthor(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	if img := AuthorBadge("", 16); img != nil {
		t.Fatal("expected nil badge for empty author")
	}
}

func TestAuthorBadgeCachesBySameNameAndSize(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	first := AuthorBadge("ann", 16)
	second := AuthorBadge("ann", 16)
	if first == nil || second == nil {
		t.Fatal("expected a non-nil badge")
	}
	if first != second {
		t.Fatal("badge for the same name+size must be cached and reused")
	}
}

func TestAuthorBadgeCacheIsKeyedBySize(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	small := AuthorBadge("ann", 12)
	big := AuthorBadge("ann", 20)
	if small.Bounds().Dx() == big.Bounds().Dx() {
		t.Fatal("badges for different sizes must not share cached bounds")
	}
}

func TestAuthorBadgeProducesASquareImageOfTheRequestedSize(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	img := AuthorBadge("ann", 16)
	b := img.Bounds()
	if b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("badge bounds = %v, want 16x16", b)
	}
}

func TestAuthorBadgeFillsWithTheAuthorColor(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	name := "ann"
	img := AuthorBadge(name, 16)
	want := authorColor(name)
	r, g, b, a := img.At(8, 8).RGBA()
	got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	if got != want {
		t.Fatalf("center pixel = %v, want %v", got, want)
	}
}

func TestInsideRoundedSquareIsAlwaysTrueWhenRadiusIsZero(t *testing.T) {
	if !insideRoundedSquare(0, 0, 10, 0) {
		t.Fatal("radius 0 must not clip any pixel")
	}
}

func TestInsideRoundedSquareIsTrueOnStraightEdgesAndInterior(t *testing.T) {
	if !insideRoundedSquare(5, 0, 16, 3) {
		t.Fatal("a pixel on the top edge outside the corner box must be inside")
	}
	if !insideRoundedSquare(0, 5, 16, 3) {
		t.Fatal("a pixel on the left edge outside the corner box must be inside")
	}
	if !insideRoundedSquare(8, 8, 16, 3) {
		t.Fatal("the center pixel must be inside")
	}
}

func TestInsideRoundedSquareClipsFarCornerPixelsForALargeRadius(t *testing.T) {
	if insideRoundedSquare(0, 0, 16, 6) {
		t.Fatal("the extreme corner pixel must be clipped for a large enough radius")
	}
}

func TestInsideRoundedSquareKeepsTheCornerArcCenterPixel(t *testing.T) {
	if !insideRoundedSquare(5, 5, 16, 6) {
		t.Fatal("the corner arc center pixel must remain inside")
	}
}
