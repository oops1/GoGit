//go:build oracle

package posixre

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var grepCorpus = []string{
	"abc",
	"aBc ABC abc",
	"foo(bar) = 1;",
	"  int main(void)",
	"x+y*z",
	"[bracket] {brace} ]close",
	"tab\there",
	"under_score and-dash",
	"a.b.c",
	"caret^ dollar$ end",
	`back\slash`,
	"ééé Привет мир",
	"aaaa",
	"abcd",
	"abbbc",
	"12345 678",
	"3.14e10",
	"hello world",
	"HELLO World",
	"a|b",
	"()",
	"{}",
	"?+",
	"word, words; wordy",
	"x{2}y",
	"zzZZzz",
	"_-_",
	"key: value",
	"\vvertical\fform",
	"MiXeD CaSe 123",
	"ab ab ab",
	"xyzxyz",
	"sub foo($) { # comment",
	"@implementation Thing",
	"\\section*{Intro}",
	"---",
}

type grepCase struct {
	pattern string
	flags   Flags
}

func grepCases() []grepCase {
	var cases []grepCase
	add := func(flags Flags, patterns ...string) {
		for _, pattern := range patterns {
			cases = append(cases, grepCase{pattern: pattern, flags: flags})
		}
	}
	add(Extended,
		`a|ab`, `(a|ab)(c|bcd)`, `ab+`, `ab*c`, `ab?c`, `b{2}`, `b{1,}`, `b{,2}c`, `b{1,2}`,
		`[[:upper:]]+`, `[[:alpha:]_]+`, `[^[:space:]]+`, `[]a]+`, `[^]a]+`, `[a-]+`, `[-a]+`, `[--/]+`,
		`\w+`, `\W+`, `\s+`, `\S+`, `\bab\b`, `\<wor`, `rd\>`, `\Bor`,
		`^ *int`, `end$`, `\$`, `\^`, `\.b`, `a\.`, `[.]c`, `\(`, `\)`, `)`, `x\{2\}`, `\{\}`,
		`[\]+`, `back\\`, `[[.-.]]+`, `[[=a=]]b`, `[[:digit:][:punct:]]+`, `é+`, `[а-я]+`, `[^а-я ]+`,
		`.+`, `(a*)*b`, `(a|b)*c`, `a**`, `a+?`, `(ab|a)(bab)?`, `x(yz)+`, `[[:xdigit:]]{3,}`,
		`[[:print:]]+`, `[[:graph:]]+`, `[[:cntrl:]]`, `[[:blank:]]+`, `[[:alnum:]]+`, `[[:lower:]]+`,
		`sub [[:alnum:]_':]+[ \t]*(\([^)]*\)[ \t]*)?`, `^@(implementation|interface)[ \t].*$`,
		`\\\\((sub)*section|chapter)\*{0,1}\{.*`, `-?[_a-zA-Z][-_a-zA-Z0-9]*`,
	)
	add(Extended|IgnoreCase,
		`abc`, `[a-c]+`, `[A-C]+`, `[[:lower:]]+`, `[[:upper:]]+`, `hello w`, `[^a-z ]+`, `mixed`, `\A`, `[_-a]`,
		`[Z-a]`, `x{2}Y`, `[[.a.]]b`, `привет`,
	)
	add(0,
		`a\|ab`, `\(a\|ab\)\(c\|bcd\)`, `ab\+`, `ab*c`, `ab\?c`, `b\{2\}`, `b\{1,\}`, `a{`, `a+`, `a?`,
		`*star`, `^*`, `a^b`, `a$b`, `\(^a\)`, `c$`, `\(c$\)`, `x\(y\)*`, `\{`, `a|b`, `()`, `{}`,
		`[[:alpha:]]\+`, `wor\(d\|ds\)`, `[^[:alnum:] ]\+`, `.*`, `\w\+`, `\<wor`,
	)
	add(IgnoreCase, `hello`, `[a-z]\+`, `a\{2\}`)
	return cases
}

func gitGrep(t *testing.T, dir string, c grepCase) (map[int]string, bool) {
	t.Helper()
	args := []string{"grep", "--no-index", "--no-color", "-h", "-n", "-o", "-a"}
	if c.flags&Extended != 0 {
		args = append(args, "-E")
	} else {
		args = append(args, "-G")
	}
	if c.flags&IgnoreCase != 0 {
		args = append(args, "-i")
	}
	args = append(args, "-e", c.pattern, "--", "corpus.txt")
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"HOME=" + dir,
		"USERPROFILE=" + dir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(dir, "no-global"),
		"GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir),
	}
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit) && exit.ExitCode() == 1 && errs.Len() == 0:
	case errors.As(err, &exit) && exit.ExitCode() == 128:
		return nil, false
	default:
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, errs.String())
	}
	first := map[int]string{}
	for line := range strings.SplitSeq(out.String(), "\n") {
		number, text, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		at, err := strconv.Atoi(number)
		if err != nil {
			t.Fatalf("unexpected grep output %q", line)
		}
		if _, seen := first[at]; !seen {
			first[at] = text
		}
	}
	return first, true
}

func TestOracleMatchesAgreeWithGitGrep(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corpus.txt"), []byte(strings.Join(grepCorpus, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range grepCases() {
		want, compiled := gitGrep(t, dir, c)
		re, err := Compile(c.pattern, c.flags|Newline)
		if !compiled {
			if err == nil {
				t.Errorf("git rejects %q (flags %d) but it compiled to %s", c.pattern, c.flags, re)
			}
			continue
		}
		if err != nil {
			t.Errorf("Compile(%q, %d) returned error %v", c.pattern, c.flags, err)
			continue
		}
		got := map[int]string{}
		for at, line := range grepCorpus {
			loc := re.FindIndex([]byte(line))
			if loc != nil && loc[1] > loc[0] {
				got[at+1] = line[loc[0]:loc[1]]
			}
		}
		for at := range len(grepCorpus) + 1 {
			if at > 0 && platformCtypeDiffers(c, grepCorpus[at-1]) {
				continue
			}
			if got[at] != want[at] {
				t.Errorf("pattern %q (flags %d, go %s) on line %d: ours %q, git %q", c.pattern, c.flags, re, at, got[at], want[at])
			}
		}
	}
}

func platformCtypeDiffers(c grepCase, line string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	high := strings.IndexFunc(line, func(r rune) bool { return r >= 0x80 }) >= 0
	classes := strings.Contains(c.pattern, "[:") || strings.ContainsAny(c.pattern, "\\")
	switch {
	case high && (classes || c.flags&IgnoreCase != 0):
		return true
	case strings.Contains(line, "\t") && (strings.Contains(c.pattern, "[:print:]") || strings.Contains(c.pattern, "[:punct:]")):
		return true
	}
	return false
}
