package pack

import (
	"os"
	"testing"
)

func TestReadingAMappedFileThatWasTruncatedReportsAnErrorInsteadOfCrashing(t *testing.T) {
	mapped, path := mappedFileWith(t, make([]byte, 3*os.Getpagesize()))
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("Truncate returned error %v", err)
	}

	_, err := mapped.ReadAt(make([]byte, 100), int64(2*os.Getpagesize()))

	if err == nil {
		t.Fatal("ReadAt of a truncated mapping returned no error")
	}
}
