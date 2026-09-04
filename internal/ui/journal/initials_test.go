package journal

import "testing"

func TestInitialsTakesFirstLetterOfEachOfTheFirstTwoCyrillicWords(t *testing.T) {
	if got := Initials("Чукалин Валерий"); got != "ЧВ" {
		t.Fatalf("Initials = %q, want %q", got, "ЧВ")
	}
}

func TestInitialsTakesFirstLetterOfEachOfTheFirstTwoLatinWords(t *testing.T) {
	if got := Initials("John Smith"); got != "JS" {
		t.Fatalf("Initials = %q, want %q", got, "JS")
	}
}

func TestInitialsIgnoresWordsAfterTheFirstTwo(t *testing.T) {
	if got := Initials("Anna Maria Muller"); got != "AM" {
		t.Fatalf("Initials = %q, want %q", got, "AM")
	}
}

func TestInitialsTakesFirstTwoLettersOfASingleLatinWord(t *testing.T) {
	if got := Initials("ann"); got != "AN" {
		t.Fatalf("Initials = %q, want %q", got, "AN")
	}
}

func TestInitialsTakesFirstTwoLettersOfASingleCyrillicWord(t *testing.T) {
	if got := Initials("аня"); got != "АН" {
		t.Fatalf("Initials = %q, want %q", got, "АН")
	}
}

func TestInitialsUppercasesASingleOneLetterWord(t *testing.T) {
	if got := Initials("x"); got != "X" {
		t.Fatalf("Initials = %q, want %q", got, "X")
	}
}

func TestInitialsOfEmptyNameIsEmpty(t *testing.T) {
	if got := Initials(""); got != "" {
		t.Fatalf("Initials = %q, want empty", got)
	}
}

func TestInitialsOfWhitespaceOnlyNameIsEmpty(t *testing.T) {
	if got := Initials("   \t  "); got != "" {
		t.Fatalf("Initials = %q, want empty", got)
	}
}

func TestInitialsTrimsExtraWhitespaceBetweenWords(t *testing.T) {
	if got := Initials("  Valeriy    Chukalin  "); got != "VC" {
		t.Fatalf("Initials = %q, want %q", got, "VC")
	}
}

func TestInitialsAreAlreadyUppercaseWhenSourceIsLowercase(t *testing.T) {
	if got := Initials("valeriy chukalin"); got != "VC" {
		t.Fatalf("Initials = %q, want %q", got, "VC")
	}
}
