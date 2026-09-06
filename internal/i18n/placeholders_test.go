package i18n

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var formatVerbPattern = regexp.MustCompile(`%(\[\d+\])?[#0+\- ]*(\d+|\*)?(\.(\d+|\*))?[a-zA-Z%]`)

func extractFormatVerbs(s string) []string {
	verbs := make([]string, 0)
	for _, m := range formatVerbPattern.FindAllString(s, -1) {
		if m == "%%" {
			continue
		}
		verbs = append(verbs, m[len(m)-1:])
	}
	return verbs
}

func TestExtractFormatVerbsOrdersAndIgnoresEscapedPercent(t *testing.T) {
	got := extractFormatVerbs("%s of %d (%%) -> %v, %q")
	want := []string{"s", "d", "v", "q"}
	if !slices.Equal(got, want) {
		t.Fatalf("extractFormatVerbs = %v, want %v", got, want)
	}
}

func TestExtractFormatVerbsNoPlaceholders(t *testing.T) {
	if got := extractFormatVerbs("plain text"); len(got) != 0 {
		t.Fatalf("extractFormatVerbs = %v, want empty", got)
	}
}

func TestPlaceholdersMatchBetweenEnglishAndRussianForSharedKeys(t *testing.T) {
	cat, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	en, ru := cat["en"], cat["ru"]
	keys := make([]string, 0, len(en))
	for key := range en {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var mismatches []string
	for _, key := range keys {
		ruValue, ok := ru[key]
		if !ok {
			continue
		}
		enValue := en[key]
		enVerbs := extractFormatVerbs(enValue)
		ruVerbs := extractFormatVerbs(ruValue)
		if slices.Equal(enVerbs, ruVerbs) {
			continue
		}
		mismatches = append(mismatches, fmt.Sprintf(
			"%s: internal/assets/i18n/en.json=%q %v vs internal/assets/i18n/ru.json=%q %v",
			key, enValue, enVerbs, ruValue, ruVerbs))
	}
	if len(mismatches) > 0 {
		t.Fatalf("placeholder sets differ between the translation tables:\n%s", strings.Join(mismatches, "\n"))
	}
}
