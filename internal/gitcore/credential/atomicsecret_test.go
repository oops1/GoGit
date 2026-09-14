package credential

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicSecretCleansUpWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "credentials")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomicSecret(target, []byte("secret")); err == nil {
		t.Fatal("writing over a directory succeeded")
	}
	if _, err := os.Stat(target + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the temporary file was left behind: %v", err)
	}
}
