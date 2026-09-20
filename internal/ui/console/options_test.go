package console

import (
	"errors"
	"slices"
	"testing"
)

var testSpecs = []option{
	plain("porcelain", ""),
	plain("short", "s"),
	plain("branch", "b"),
	valued("message", "m"),
	valued("max-count", "n"),
}

func TestParseOptionsUnderstandsGitStyleFlags(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		seen  map[string]string
		rest  []string
		split bool
	}{
		{name: "nothing", args: nil, seen: map[string]string{}},
		{name: "long flag", args: []string{"--porcelain"}, seen: map[string]string{"porcelain": ""}},
		{name: "short flag", args: []string{"-s"}, seen: map[string]string{"short": ""}},
		{name: "bundled short flags", args: []string{"-sb"}, seen: map[string]string{"short": "", "branch": ""}},
		{name: "long value with equals", args: []string{"--message=hi"}, seen: map[string]string{"message": "hi"}},
		{name: "long value as the next word", args: []string{"--message", "hi"}, seen: map[string]string{"message": "hi"}},
		{name: "short value as the next word", args: []string{"-m", "hi"}, seen: map[string]string{"message": "hi"}},
		{name: "short value glued to the letter", args: []string{"-n5"}, seen: map[string]string{"max-count": "5"}},
		{name: "value at the end of a bundle", args: []string{"-sn5"}, seen: map[string]string{"short": "", "max-count": "5"}},
		{name: "positional words", args: []string{"a", "b"}, seen: map[string]string{}, rest: []string{"a", "b"}},
		{name: "a lone dash is positional", args: []string{"-"}, seen: map[string]string{}, rest: []string{"-"}},
		{
			name: "everything after two dashes is positional",
			args: []string{"--porcelain", "--", "--porcelain", "-s"},
			seen: map[string]string{"porcelain": ""},
			rest: []string{"--porcelain", "-s"}, split: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseOptions(tt.args, testSpecs)
			if err != nil {
				t.Fatalf("parseOptions(%v) returned error %v", tt.args, err)
			}
			for long, want := range tt.seen {
				if !opts.has(long) || opts.value(long) != want {
					t.Fatalf("%s = %q (present %v), want %q", long, opts.value(long), opts.has(long), want)
				}
			}
			if len(opts.seen) != len(tt.seen) {
				t.Fatalf("seen = %v, want %v", opts.seen, tt.seen)
			}
			if !slices.Equal(opts.args(), tt.rest) {
				t.Fatalf("rest = %#v, want %#v", opts.args(), tt.rest)
			}
			before, after := opts.split()
			if tt.split && !slices.Equal(after, tt.rest) {
				t.Fatalf("after = %#v, want %#v", after, tt.rest)
			}
			if !tt.split && (len(after) != 0 || !slices.Equal(before, tt.rest)) {
				t.Fatalf("before = %#v, after = %#v", before, after)
			}
		})
	}
}

func TestParseOptionsRefusesBadFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want error
		text string
	}{
		{"unknown long flag", []string{"--nope"}, ErrUnknownOption, "--nope"},
		{"unknown short flag", []string{"-z"}, ErrUnknownOption, "-z"},
		{"unknown letter inside a bundle", []string{"-sz"}, ErrUnknownOption, "-z"},
		{"value given to a plain flag", []string{"--porcelain=1"}, ErrOptionTakesNoValue, "--porcelain=1"},
		{"long option without its value", []string{"--message"}, ErrOptionNeedsValue, "--message"},
		{"short option without its value", []string{"-m"}, ErrOptionNeedsValue, "-m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args, testSpecs)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			var d *Detail
			if !errors.As(err, &d) || d.Text != tt.text {
				t.Fatalf("detail = %+v, want %q", d, tt.text)
			}
		})
	}
}

func TestAnyOfFindsTheFirstPresentFlag(t *testing.T) {
	opts, err := parseOptions([]string{"-b"}, testSpecs)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.anyOf("short", "branch") {
		t.Fatal("anyOf must see the branch flag")
	}
	if opts.anyOf("short", "porcelain") {
		t.Fatal("anyOf must not invent flags")
	}
}

func TestDetailErrorSpellsOutTheOffendingWord(t *testing.T) {
	if got := detail(ErrUsage, "add").Error(); got != ErrUsage.Error()+": add" {
		t.Fatalf("Error() = %q", got)
	}
	if got := detail(ErrUsage, "").Error(); got != ErrUsage.Error() {
		t.Fatalf("Error() = %q", got)
	}
}
