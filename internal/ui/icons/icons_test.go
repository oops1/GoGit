package icons

import (
	"errors"
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

func TestConcurrentAccessIsSafe(t *testing.T) {
	resetForTest()
	names := []string{"added", "modified", "conflict", "does-not-exist"}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Go(func() {
			for _, name := range names {
				Status(name, 16)
				Tree("branch", 16)
			}
		})
	}
	wg.Wait()
}
