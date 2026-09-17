package userdiff

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/posixre"
)

var gitBuiltinNames = []string{
	"ada", "bash", "bibtex", "cpp", "csharp", "css", "dts", "elixir", "fortran", "fountain",
	"golang", "html", "java", "kotlin", "markdown", "matlab", "objc", "pascal", "perl", "php",
	"python", "ruby", "rust", "scheme", "tex",
}

func TestBuiltinsListEveryDriverOfGit(t *testing.T) {
	var names []string
	for _, driver := range Builtins() {
		names = append(names, driver.Name)
	}
	if !slices.Equal(names, gitBuiltinNames) {
		t.Fatalf("Builtins() = %v", names)
	}
	copied := Builtins()
	copied[0].Name = "changed"
	if Builtins()[0].Name != "ada" {
		t.Fatal("Builtins returned the shared table")
	}
}

func TestBuiltinPatternsCompile(t *testing.T) {
	for _, driver := range Builtins() {
		if _, err := Compile(driver.FuncName); err != nil {
			t.Errorf("%s funcname: %v", driver.Name, err)
		}
		if _, err := posixre.Compile(driver.WordRegex, posixre.Extended|posixre.Newline); err != nil {
			t.Errorf("%s word regex: %v", driver.Name, err)
		}
	}
}

func TestBuiltinFindsADriverByItsExactName(t *testing.T) {
	driver, ok := Builtin("golang")
	if !ok || driver.FuncName.Flags != posixre.Extended {
		t.Fatalf("Builtin(golang) = %+v, %v", driver, ok)
	}
	if driver, ok := Builtin("css"); !ok || driver.FuncName.Flags != posixre.Extended|posixre.IgnoreCase {
		t.Fatalf("Builtin(css) = %+v, %v", driver, ok)
	}
	for _, name := range []string{"Golang", "default", "go"} {
		if _, ok := Builtin(name); ok {
			t.Errorf("Builtin(%q) found a driver", name)
		}
	}
}

func loadConfig(t *testing.T, text string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{GlobalFile: path, NoSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadAppliesDriverSettingsInConfigOrder(t *testing.T) {
	cfg := loadConfig(t, "[diff \"cpp\"]\n\txfuncname = ^int (.*)$\n"+
		"[diff \"mine\"]\n\tfuncname = ^\\\\(sub\\\\) .*\n\twordRegex = [a-z]+\n"+
		"[diff \"both\"]\n\tfuncname = basic\n\txfuncname = extended\n"+
		"[diff \"reversed\"]\n\txfuncname = extended\n\tfuncname = basic\n"+
		"[diff \"empty\"]\n\tbinary = true\n\txfuncname\n"+
		"[diff]\n\tfuncname = ignored\n"+
		"[merge \"cpp\"]\n\txfuncname = ignored\n")
	drivers := Load(cfg)
	cases := []struct {
		name   string
		source string
		flags  posixre.Flags
		words  string
	}{
		{name: "cpp", source: "^int (.*)$", flags: posixre.Extended},
		{name: "mine", source: `^\(sub\) .*`, words: "[a-z]+"},
		{name: "both", source: "extended", flags: posixre.Extended},
		{name: "reversed", source: "basic"},
		{name: "empty"},
		{name: "python", source: builtinDriver("python").FuncName.Source, flags: posixre.Extended},
	}
	for _, c := range cases {
		driver, ok := drivers.Driver(c.name)
		if !ok || driver.FuncName.Source != c.source || driver.FuncName.Flags != c.flags {
			t.Errorf("Driver(%q) = %+v, %v", c.name, driver, ok)
		}
		if c.words != "" && driver.WordRegex != c.words {
			t.Errorf("Driver(%q) word regex = %q", c.name, driver.WordRegex)
		}
	}
	if _, ok := drivers.Driver("missing"); ok {
		t.Error("an unknown driver was found")
	}
	if builtin, _ := Builtin("cpp"); builtin.FuncName.Source == "^int (.*)$" {
		t.Error("configuration changed the built-in table")
	}
	if _, ok := Load(nil).Driver("ada"); !ok {
		t.Error("drivers without configuration lost the built-in ones")
	}
}

func builtinDriver(name string) Driver {
	driver, _ := Builtin(name)
	return driver
}

func TestMatcherIsCompiledOnceAndReportsBadPatterns(t *testing.T) {
	drivers := Load(loadConfig(t, "[diff \"bad\"]\n\txfuncname = (\n[diff \"plain\"]\n\tbinary = true\n"))
	first, err := drivers.Matcher("golang")
	if err != nil || first == nil {
		t.Fatalf("Matcher(golang) = %v, %v", first, err)
	}
	if again, _ := drivers.Matcher("golang"); again != first {
		t.Error("the matcher was compiled twice")
	}
	for _, name := range []string{"plain", "unknown"} {
		if m, err := drivers.Matcher(name); m != nil || err != nil {
			t.Errorf("Matcher(%q) = %v, %v", name, m, err)
		}
	}
	for range 2 {
		if _, err := drivers.Matcher("bad"); !errors.Is(err, posixre.ErrPattern) {
			t.Errorf("Matcher(bad) returned %v", err)
		}
	}
}

func TestCompileRejectsANegatedLastExpression(t *testing.T) {
	if _, err := Compile(Pattern{Source: "a\n!b", Flags: posixre.Extended}); !errors.Is(err, ErrLastNegated) {
		t.Fatalf("Compile returned %v", err)
	}
	if _, err := Compile(Pattern{Source: "!a\n[", Flags: posixre.Extended}); !errors.Is(err, posixre.ErrPattern) {
		t.Fatalf("Compile returned %v", err)
	}
}

func TestMatcherTakesTheFirstGroupOrTheWholeMatch(t *testing.T) {
	m, err := Compile(Pattern{Source: "!skip\n^ *(def [a-z]+)\nclass [A-Z]\\w*", Flags: posixre.Extended})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		line   string
		header string
		ok     bool
	}{
		{line: "  def name(x):\n", header: "def name", ok: true},
		{line: "class Thing(Base):\r\n", header: "class Thing", ok: true},
		{line: "def skip():\n", ok: false},
		{line: "plain text\n", ok: false},
		{line: "  def word\r", header: "def word", ok: true},
	}
	for _, c := range cases {
		got, ok := m.Header([]byte(c.line), HeaderLimit)
		if got != c.header || ok != c.ok {
			t.Errorf("Header(%q) = %q, %v", c.line, got, ok)
		}
	}
}

func TestHeaderTruncatesBeforeTrimmingGitSpaces(t *testing.T) {
	long := "func abcdefghij"
	for len(long) < 78 {
		long += "abcdefghij"
	}
	long = long[:78] + "  tail\n"
	var none *Matcher
	cases := []struct {
		line   string
		limit  int
		header string
		ok     bool
	}{
		{line: long, limit: HeaderLimit, header: long[:78], ok: true},
		{line: "name \v\f \t\r\n", limit: HeaderLimit, header: "name \v\f", ok: true},
		{line: "_x\n", limit: 1, header: "_", ok: true},
		{line: "$dollar", limit: HeaderLimit, header: "$dollar", ok: true},
		{line: "Zed", limit: HeaderLimit, header: "Zed", ok: true},
		{line: " indented\n", limit: HeaderLimit},
		{line: "", limit: HeaderLimit},
		{line: "1digit", limit: HeaderLimit},
		{line: "\xc3\xa9t\xc3\xa9", limit: HeaderLimit},
	}
	for _, c := range cases {
		got, ok := none.Header([]byte(c.line), c.limit)
		if got != c.header || ok != c.ok {
			t.Errorf("Header(%q, %d) = %q, %v", c.line, c.limit, got, ok)
		}
	}
}
