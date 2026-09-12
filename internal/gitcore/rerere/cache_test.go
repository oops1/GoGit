package rerere

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const testID = "0123456789abcdef0123456789abcdef01234567"

func newTestCache(t *testing.T) (*Cache, string) {
	t.Helper()
	gitDir := t.TempDir()
	cache, err := OpenCache(gitDir)
	if err != nil {
		t.Fatalf("OpenCache returned error %v", err)
	}
	return cache, gitDir
}

func TestOpenCacheMakesTheDirectoryAndExistsSeesIt(t *testing.T) {
	gitDir := t.TempDir()
	if CacheExists(gitDir) {
		t.Fatal("the cache exists before it was opened")
	}

	cache, err := OpenCache(gitDir)
	if err != nil {
		t.Fatalf("OpenCache returned error %v", err)
	}

	if !CacheExists(gitDir) || cache.Dir() != filepath.Join(gitDir, CacheDir) {
		t.Fatalf("dir = %s", cache.Dir())
	}
}

func TestOpenCacheFailsWhenTheDirectoryIsAFile(t *testing.T) {
	gitDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gitDir, CacheDir), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenCache(gitDir); err == nil {
		t.Fatal("a file was taken for the cache directory")
	}
	if CacheExists(gitDir) {
		t.Fatal("a file was taken for the cache directory")
	}
}

func TestTheCacheKeepsPreimagesAndPostimagesApartByVariant(t *testing.T) {
	cache, _ := newTestCache(t)

	if err := cache.WritePreimage(testID, 0, []byte("pre\n")); err != nil {
		t.Fatalf("WritePreimage returned error %v", err)
	}
	if err := cache.WritePostimage(testID, 0, []byte("post\n")); err != nil {
		t.Fatalf("WritePostimage returned error %v", err)
	}
	if err := cache.WritePreimage(testID, 2, []byte("pre two\n")); err != nil {
		t.Fatalf("WritePreimage returned error %v", err)
	}
	if err := cache.WriteThisimage(testID, 0, []byte("this\n")); err != nil {
		t.Fatalf("WriteThisimage returned error %v", err)
	}

	variants := cache.Variants(testID)
	want := []Variant{{Index: 0, HasPreimage: true, HasPostimage: true}, {Index: 2, HasPreimage: true}}
	if !slices.Equal(variants, want) {
		t.Fatalf("variants = %+v", variants)
	}
	if !variants[0].Complete() || variants[1].Complete() {
		t.Fatalf("variants = %+v", variants)
	}
	preimage, err := cache.Preimage(testID, 2)
	if err != nil || string(preimage) != "pre two\n" {
		t.Fatalf("preimage = %q, %v", preimage, err)
	}
	postimage, err := cache.Postimage(testID, 0)
	if err != nil || string(postimage) != "post\n" {
		t.Fatalf("postimage = %q, %v", postimage, err)
	}
}

func TestVariantsOfAnUnknownConflictAreEmpty(t *testing.T) {
	cache, _ := newTestCache(t)

	if variants := cache.Variants(testID); variants != nil {
		t.Fatalf("variants = %+v", variants)
	}
}

func TestVariantsSkipFilesThatAreNotImages(t *testing.T) {
	cache, gitDir := newTestCache(t)
	if err := os.MkdirAll(filepath.Join(gitDir, CacheDir, testID), 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"preimage.bogus", "notes", "preimage.0", "postimage.-1"} {
		if err := os.WriteFile(filepath.Join(gitDir, CacheDir, testID, name), nil, 0o666); err != nil {
			t.Fatal(err)
		}
	}

	if variants := cache.Variants(testID); len(variants) != 0 {
		t.Fatalf("variants = %+v", variants)
	}
}

func TestAPreimageGoesIntoTheFirstVariantWithoutAResolution(t *testing.T) {
	cache, _ := newTestCache(t)

	if got := cache.VariantForAPreimage(testID); got != 0 {
		t.Fatalf("the first preimage goes to variant %d", got)
	}
	if err := cache.WritePreimage(testID, 0, nil); err != nil {
		t.Fatal(err)
	}
	if got := cache.VariantForAPreimage(testID); got != 0 {
		t.Fatalf("a variant without a resolution was not reused: %d", got)
	}
	if err := cache.WritePostimage(testID, 0, nil); err != nil {
		t.Fatal(err)
	}
	if got := cache.VariantForAPreimage(testID); got != 1 {
		t.Fatalf("a resolved variant was reused: %d", got)
	}
}

func TestRemoveVariantTakesEveryImageWithIt(t *testing.T) {
	cache, _ := newTestCache(t)
	if err := cache.WritePreimage(testID, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := cache.WritePostimage(testID, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteThisimage(testID, 1, nil); err != nil {
		t.Fatal(err)
	}

	if err := cache.RemoveVariant(testID, 1); err != nil {
		t.Fatalf("RemoveVariant returned error %v", err)
	}
	if err := cache.RemoveVariant(testID, 1); err != nil {
		t.Fatalf("removing twice returned error %v", err)
	}

	if variants := cache.Variants(testID); len(variants) != 0 {
		t.Fatalf("variants = %+v", variants)
	}
}

func TestAMissingImageIsReportedAsMissing(t *testing.T) {
	cache, _ := newTestCache(t)

	if _, err := cache.Preimage(testID, 0); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
	if _, err := cache.Postimage(testID, 3); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeRRRoundTripsThroughItsFileFormat(t *testing.T) {
	entries := []Entry{{ID: testID, Path: "f"}, {ID: testID, Variant: 2, Path: "dir/g"}}

	data := FormatMergeRR(entries)
	back, err := ParseMergeRR(data)

	if err != nil {
		t.Fatalf("ParseMergeRR returned error %v", err)
	}
	if string(data) != testID+"\tf\x00"+testID+".2\tdir/g\x00" {
		t.Fatalf("data = %q", data)
	}
	if !slices.Equal(back, entries) {
		t.Fatalf("entries = %+v", back)
	}
}

func TestParseMergeRRRefusesBrokenRecords(t *testing.T) {
	for _, data := range []string{
		"no tab here\x00",
		testID + "\t\x00",
		testID + ".bogus\tf\x00",
		".2\tf\x00",
	} {
		if _, err := ParseMergeRR([]byte(data)); !errors.Is(err, ErrCorruptMergeRR) {
			t.Errorf("%q: err = %v", data, err)
		}
	}
}

func TestParseMergeRRAcceptsAnEmptyFile(t *testing.T) {
	entries, err := ParseMergeRR(nil)

	if err != nil || entries != nil {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
}

func TestTheCacheRefusesAnIDThatIsNotAHash(t *testing.T) {
	cache, _ := newTestCache(t)

	for _, id := range []string{"", "../escape", "ABCDEF", "0123456789abcdefg"} {
		if variants := cache.Variants(id); variants != nil {
			t.Errorf("%q: Variants = %+v", id, variants)
		}
		if _, err := cache.Preimage(id, 0); !errors.Is(err, ErrNotAConflictID) {
			t.Errorf("%q: Preimage = %v", id, err)
		}
		if err := cache.WritePreimage(id, 0, nil); !errors.Is(err, ErrNotAConflictID) {
			t.Errorf("%q: WritePreimage = %v", id, err)
		}
		if err := cache.RemoveVariant(id, 0); !errors.Is(err, ErrNotAConflictID) {
			t.Errorf("%q: RemoveVariant = %v", id, err)
		}
	}
}

func TestTheCacheReportsAFileWhereAConflictDirectoryBelongs(t *testing.T) {
	cache, gitDir := newTestCache(t)
	if err := os.WriteFile(filepath.Join(gitDir, CacheDir, testID), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if variants := cache.Variants(testID); variants != nil {
		t.Fatalf("variants = %+v", variants)
	}
	if err := cache.WritePreimage(testID, 0, nil); err == nil {
		t.Fatal("a preimage was written into a file")
	}
}

func TestRemoveVariantReportsWhatItCannotRemove(t *testing.T) {
	cache, gitDir := newTestCache(t)
	stubborn := filepath.Join(gitDir, CacheDir, testID, preimageName, "inside")
	if err := os.MkdirAll(stubborn, 0o777); err != nil {
		t.Fatal(err)
	}

	if err := cache.RemoveVariant(testID, 0); err == nil {
		t.Fatal("a directory with files in it was removed")
	}
}
