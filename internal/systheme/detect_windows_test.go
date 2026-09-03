package systheme

import (
	"errors"
	"testing"
)

func TestFromAppsUseLightTheme(t *testing.T) {
	if fromAppsUseLightTheme(0, nil) != Dark {
		t.Fatal("0 must be dark")
	}
	if fromAppsUseLightTheme(1, nil) != Light {
		t.Fatal("1 must be light")
	}
	if fromAppsUseLightTheme(1, errors.New("x")) != Unknown {
		t.Fatal("error must be unknown")
	}
}

func TestReadIntegerValueErrors(t *testing.T) {
	if _, err := readIntegerValue(`Software\GoGit\DoesNotExist\Key`, "x"); err == nil {
		t.Fatal("missing key must fail")
	}
	if _, err := readIntegerValue(personalizeKey, "GoGitMissingValue"); err == nil {
		t.Fatal("missing value must fail")
	}
}

func TestDetectReadsRegistry(t *testing.T) {
	value, err := readIntegerValue(personalizeKey, "AppsUseLightTheme")
	got := Detect()
	if got != fromAppsUseLightTheme(value, err) {
		t.Fatalf("Detect() = %v, registry says %d (%v)", got, value, err)
	}
}
