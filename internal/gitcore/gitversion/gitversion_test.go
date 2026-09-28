package gitversion

import "testing"

func TestParseTakesTheVersionOutOfWhatGitPrints(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		major int
		minor int
		ok    bool
	}{
		{"linux", "git version 2.30.2", 2, 30, true},
		{"windows", "git version 2.55.0.windows.5", 2, 55, true},
		{"apple", "git version 2.39.5 (Apple Git-154)", 2, 39, true},
		{"two parts", "git version 3.0", 3, 0, true},
		{"trailing newline", "git version 2.47.1\n", 2, 47, true},
		{"no version at all", "git is not here", 0, 0, false},
		{"single number", "git version 2", 0, 0, false},
		{"not a number", "git version x.y.z", 0, 0, false},
		{"empty", "", 0, 0, false},
		{"minor is not a number", "git version 2.x.1", 0, 0, false},
		{"a number without a dot before the version", "git 2 version 2.39.5", 2, 39, true},
		{"a word starting with a digit", "git version 2nd 2.39.5", 2, 39, true},
		{"a digit word with a dot", "git version 2x.3 2.39.5", 2, 39, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			major, minor, ok := Parse(c.text)
			if major != c.major || minor != c.minor || ok != c.ok {
				t.Fatalf("Parse(%q) = %d, %d, %v, want %d, %d, %v", c.text, major, minor, ok, c.major, c.minor, c.ok)
			}
		})
	}
}

func TestAtLeastComparesTheNumbersAndTrustsWhatItCannotRead(t *testing.T) {
	cases := []struct {
		text  string
		major int
		minor int
		want  bool
	}{
		{"git version 2.30.2", 2, 30, true},
		{"git version 2.30.2", 2, 31, false},
		{"git version 2.47.1", 2, 34, true},
		{"git version 3.0.0", 2, 99, true},
		{"git version 1.9.5", 2, 0, false},
		{"git is not here", 2, 34, true},
	}
	for _, c := range cases {
		if got := AtLeast(c.text, c.major, c.minor); got != c.want {
			t.Fatalf("AtLeast(%q, %d, %d) = %v, want %v", c.text, c.major, c.minor, got, c.want)
		}
	}
}
