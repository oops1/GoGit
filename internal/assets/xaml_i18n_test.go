package assets

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var locKeyPattern = regexp.MustCompile(`\{Loc\s+([^}]+)\}`)
var exactLocBindingPattern = regexp.MustCompile(`^\{Loc\s+\S[^}]*\}$`)
var userFacingAttrPattern = regexp.MustCompile(`\b(Content|Header|Text|Title|ToolTip|Placeholder)="([^"]*)"`)

func readAllXAML(t *testing.T) map[string]string {
	files := map[string]string{}
	err := fs.WalkDir(uiFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".xaml") {
			return nil
		}
		data, err := uiFS.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no .xaml files found in the embedded ui filesystem")
	}
	return files
}

func loadRawI18NTable(t *testing.T, code string) map[string]string {
	data, err := fs.ReadFile(I18N(), code+".json")
	if err != nil {
		t.Fatalf("reading %s.json: %v", code, err)
	}
	var table map[string]string
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatalf("parsing %s.json: %v", code, err)
	}
	return table
}

func sortedXAMLNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestXAMLLocKeysExistInBothLanguageTables(t *testing.T) {
	en := loadRawI18NTable(t, "en")
	ru := loadRawI18NTable(t, "ru")
	files := readAllXAML(t)
	var problems []string
	for _, name := range sortedXAMLNames(files) {
		for _, m := range locKeyPattern.FindAllStringSubmatch(files[name], -1) {
			key := strings.TrimSpace(m[1])
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
			problems = append(problems, fmt.Sprintf("%s: {Loc %s} is missing from %s",
				name, key, strings.Join(missingFrom, " and ")))
		}
	}
	if len(problems) > 0 {
		t.Fatalf("XAML references localization keys absent from the translation tables:\n%s",
			strings.Join(problems, "\n"))
	}
}

func TestXAMLHasNoLiteralUserFacingText(t *testing.T) {
	files := readAllXAML(t)
	var problems []string
	for _, name := range sortedXAMLNames(files) {
		for _, m := range userFacingAttrPattern.FindAllStringSubmatch(files[name], -1) {
			attr, value := m[1], m[2]
			if value == "" || exactLocBindingPattern.MatchString(value) {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s: %s=%q is a literal string instead of {Loc ...}",
				name, attr, value))
		}
	}
	if len(problems) > 0 {
		t.Fatalf("XAML contains literal user-facing text outside of {Loc ...} bindings:\n%s",
			strings.Join(problems, "\n"))
	}
}
