package config

import (
	"errors"
	"testing"
)

func TestExtensionsAcceptTheOnesThatChangeNothingForUs(t *testing.T) {
	text := "[extensions]\n\tnoop = true\n\tnoop-v1 = true\n\trelativeWorktrees = true\n\trefStorage = files\n"
	if _, err := loadText(t, text).Extensions(); err != nil {
		t.Fatalf("Extensions returned %v", err)
	}
}

func TestExtensionsRejectValuesWeCannotHonour(t *testing.T) {
	cases := map[string]error{
		"[extensions]\n\trefStorage = reftable\n":     ErrUnknownExtension,
		"[extensions]\n\trelativeWorktrees = maybe\n": nil,
	}
	for text, want := range cases {
		if _, err := loadText(t, text).Extensions(); err == nil || want != nil && !errors.Is(err, want) {
			t.Fatalf("Extensions for %q returned %v, want %v", text, err, want)
		}
	}
}
