package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const i18nPackageImportPath = "github.com/oops1/gogit/internal/i18n"

func moduleRoot(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not report the test file location")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", file)
		}
		dir = parent
	}
}

func skippedSourceDir(name string) bool {
	switch name {
	case ".git", ".github", ".idea", "bin", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func i18nLocalName(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != i18nPackageImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "i18n"
	}
	return ""
}

func literalKeyArg(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	key, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return key, true
}

func collectGoI18nLiteralKeyUsages(t *testing.T, root string) map[string][]string {
	fset := token.NewFileSet()
	usages := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skippedSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		localName := i18nLocalName(file)
		if localName == "" {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != localName {
				return true
			}
			if sel.Sel.Name != "T" && sel.Sel.Name != "Tf" {
				return true
			}
			key, ok := literalKeyArg(call)
			if !ok {
				return true
			}
			pos := fset.Position(call.Args[0].Pos())
			rel, relErr := filepath.Rel(root, pos.Filename)
			if relErr != nil {
				rel = pos.Filename
			}
			loc := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line)
			usages[key] = append(usages[key], loc)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return usages
}

func TestGoSourceI18nLiteralKeysExistInBothTables(t *testing.T) {
	root := moduleRoot(t)
	usages := collectGoI18nLiteralKeyUsages(t, root)
	if len(usages) == 0 {
		t.Fatal("no i18n.T/i18n.Tf literal call sites were found; the scanner is broken")
	}
	cat, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	en, ru := cat["en"], cat["ru"]
	keys := make([]string, 0, len(usages))
	for key := range usages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var problems []string
	for _, key := range keys {
		_, inEn := en[key]
		_, inRu := ru[key]
		if inEn && inRu {
			continue
		}
		var missingFrom []string
		if !inEn {
			missingFrom = append(missingFrom, "internal/assets/i18n/en.json")
		}
		if !inRu {
			missingFrom = append(missingFrom, "internal/assets/i18n/ru.json")
		}
		locations := usages[key]
		sort.Strings(locations)
		problems = append(problems, fmt.Sprintf("%q used at %s is missing from %s", key,
			strings.Join(locations, ", "), strings.Join(missingFrom, " and ")))
	}
	if len(problems) > 0 {
		t.Fatalf("i18n keys referenced from Go source but absent from the translation tables:\n%s",
			strings.Join(problems, "\n"))
	}
}

var dynamicallyBuiltKeyPrefixes = []string{
	"Dialog.AddRepo.Hint.",
	"Files.Column.",
	"Files.Filter.Status.",
	"Menu.",
	"Operation.Log.",
}

func TestKeysWithDynamicPrefixesArePresentInBothLanguages(t *testing.T) {
	cat, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	en, ru := cat["en"], cat["ru"]
	for _, prefix := range dynamicallyBuiltKeyPrefixes {
		matched := 0
		for key := range en {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			matched++
			if _, ok := ru[key]; !ok {
				t.Errorf("dynamic-prefix key %q (prefix %q) is in internal/assets/i18n/en.json but missing from internal/assets/i18n/ru.json", key, prefix)
			}
		}
		for key := range ru {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			if _, ok := en[key]; !ok {
				t.Errorf("dynamic-prefix key %q (prefix %q) is in internal/assets/i18n/ru.json but missing from internal/assets/i18n/en.json", key, prefix)
			}
		}
		if matched == 0 {
			t.Errorf("dynamic key prefix %q matches no key in internal/assets/i18n/en.json; update dynamicallyBuiltKeyPrefixes in internal/i18n/modulescan_test.go", prefix)
		}
	}
}

var keyShapedLiteralPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*(\.[A-Z][A-Za-z0-9]*)+$`)

var keyShapedLiteralsThatAreNotI18nKeys = map[string]bool{
	"Go.Git": true,
}

func collectKeyShapedGoLiterals(t *testing.T, root string) map[string][]string {
	fset := token.NewFileSet()
	usages := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skippedSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil || !keyShapedLiteralPattern.MatchString(value) || keyShapedLiteralsThatAreNotI18nKeys[value] {
				return true
			}
			pos := fset.Position(lit.Pos())
			rel, relErr := filepath.Rel(root, pos.Filename)
			if relErr != nil {
				rel = pos.Filename
			}
			loc := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line)
			usages[value] = append(usages[value], loc)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return usages
}

func TestKeyShapedGoLiteralsExistInBothTables(t *testing.T) {
	root := moduleRoot(t)
	usages := collectKeyShapedGoLiterals(t, root)
	if len(usages) == 0 {
		t.Fatal("no key-shaped string literals were found; the scanner is broken")
	}
	cat, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	en, ru := cat["en"], cat["ru"]
	keys := make([]string, 0, len(usages))
	for key := range usages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var problems []string
	for _, key := range keys {
		_, inEn := en[key]
		_, inRu := ru[key]
		if inEn && inRu {
			continue
		}
		var missingFrom []string
		if !inEn {
			missingFrom = append(missingFrom, "internal/assets/i18n/en.json")
		}
		if !inRu {
			missingFrom = append(missingFrom, "internal/assets/i18n/ru.json")
		}
		locations := usages[key]
		sort.Strings(locations)
		problems = append(problems, fmt.Sprintf("%q used at %s is missing from %s", key,
			strings.Join(locations, ", "), strings.Join(missingFrom, " and ")))
	}
	if len(problems) > 0 {
		t.Fatalf("Go source has string literals shaped like i18n keys (Dotted.Pascal.Case) that are absent from the translation tables; this catches keys stored as data (menu trees, lookup maps) instead of passed straight to i18n.T/Tf:\n%s",
			strings.Join(problems, "\n"))
	}
}
