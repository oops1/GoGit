package console

import (
	"errors"
	"slices"
	"testing"
)

func TestSplitBreaksTheLineTheWayGitDoes(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"empty", "", nil},
		{"only spaces", "   \t\r\n ", nil},
		{"plain words", "status --porcelain", []string{"status", "--porcelain"}},
		{"extra spaces", "  log   --oneline  ", []string{"log", "--oneline"}},
		{"single quotes", "commit -m 'a message'", []string{"commit", "-m", "a message"}},
		{"double quotes", `commit -m "a message"`, []string{"commit", "-m", "a message"}},
		{"empty quoted word", `add ""`, []string{"add", ""}},
		{"quote glued to a word", `add "a"b`, []string{"add", "ab"}},
		{"escape inside double quotes", `commit -m "say \"hi\""`, []string{"commit", "-m", `say "hi"`}},
		{"backslash inside double quotes", `commit -m "a\\b"`, []string{"commit", "-m", `a\b`}},
		{"no escapes inside single quotes", `commit -m 'a\b'`, []string{"commit", "-m", `a\b`}},
		{"escaped space", `add my\ file.txt`, []string{"add", "my file.txt"}},
		{"space inside single quotes", "add 'my file.txt'", []string{"add", "my file.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.line)
			if err != nil {
				t.Fatalf("Split(%q) returned error %v", tt.line, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Split(%q) = %#v, want %#v", tt.line, got, tt.want)
			}
		})
	}
}

func TestSplitRefusesAnUnfinishedLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want error
	}{
		{"open single quote", "commit -m 'oops", ErrBadQuoting},
		{"open double quote", `commit -m "oops`, ErrBadQuoting},
		{"trailing backslash", `add file\`, ErrBadEscape},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Split(tt.line); !errors.Is(err, tt.want) {
				t.Fatalf("Split(%q) error = %v, want %v", tt.line, err, tt.want)
			}
		})
	}
}

func TestParseTakesTheCommandNameAndItsArguments(t *testing.T) {
	cmd, err := Parse("  log --oneline -5 ")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "log" || !slices.Equal(cmd.Args, []string{"--oneline", "-5"}) {
		t.Fatalf("Parse = %+v", cmd)
	}
}

func TestParseDropsALeadingGitWord(t *testing.T) {
	cmd, err := Parse("git status --porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "status" || !slices.Equal(cmd.Args, []string{"--porcelain"}) {
		t.Fatalf("Parse = %+v", cmd)
	}
}

func TestParseReportsAnEmptyLine(t *testing.T) {
	for _, line := range []string{"", "   ", "git", "git  "} {
		cmd, err := Parse(line)
		if err != nil {
			t.Fatalf("Parse(%q) returned error %v", line, err)
		}
		if !cmd.Empty() {
			t.Fatalf("Parse(%q) = %+v, want an empty command", line, cmd)
		}
	}
}

func TestParsePassesTheSplitErrorThrough(t *testing.T) {
	if _, err := Parse("commit -m 'oops"); !errors.Is(err, ErrBadQuoting) {
		t.Fatalf("err = %v", err)
	}
}
