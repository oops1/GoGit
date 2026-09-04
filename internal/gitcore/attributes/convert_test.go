package attributes

import (
	"bytes"
	"testing"
)

func TestCRLFActionNamesMatchGitAttributeSpelling(t *testing.T) {
	tests := []struct {
		name   string
		action CRLFAction
		want   string
	}{
		{"undefined", CRLFUndefined, ""},
		{"binary", CRLFBinary, "-text"},
		{"text", CRLFText, "text"},
		{"textInput", CRLFTextInput, "text eol=lf"},
		{"textCRLF", CRLFTextCRLF, "text eol=crlf"},
		{"auto", CRLFAuto, "text=auto"},
		{"autoInput", CRLFAutoInput, "text=auto eol=lf"},
		{"autoCRLF", CRLFAutoCRLF, "text=auto eol=crlf"},
		{"unknown", CRLFAction(200), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.action.String(); got != tc.want {
				t.Fatalf("CRLFAction(%d).String() = %q, want %q", tc.action, got, tc.want)
			}
		})
	}
}

func TestConversionNames(t *testing.T) {
	tests := []struct {
		name       string
		conversion Conversion
		want       string
	}{
		{"none", ConvertNone, "none"},
		{"lf", ConvertLF, "lf"},
		{"crlf", ConvertCRLF, "crlf"},
		{"unknown", Conversion(9), "none"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conversion.String(); got != tc.want {
				t.Fatalf("Conversion(%d).String() = %q, want %q", tc.conversion, got, tc.want)
			}
		})
	}
}

func TestTextPolicyCombinesAttributesWithConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		attributes string
		autoCRLF   string
		eol        string
		platform   string
		attr       CRLFAction
		effective  CRLFAction
		checkout   Conversion
		checkin    Conversion
		detect     bool
	}{
		{
			name: "noAttributesNoConfiguration", attributes: "", platform: EOLLF,
			attr: CRLFUndefined, effective: CRLFBinary, checkout: ConvertNone, checkin: ConvertNone,
		},
		{
			name: "noAttributesAutoCRLFTrue", attributes: "", autoCRLF: AutoCRLFTrue, platform: EOLLF,
			attr: CRLFUndefined, effective: CRLFAutoCRLF, checkout: ConvertCRLF, checkin: ConvertLF, detect: true,
		},
		{
			name: "noAttributesAutoCRLFInput", attributes: "", autoCRLF: AutoCRLFInput, platform: EOLLF,
			attr: CRLFUndefined, effective: CRLFAutoInput, checkout: ConvertLF, checkin: ConvertLF, detect: true,
		},
		{
			name: "textOnPlatformWithLF", attributes: "* text\n", platform: EOLLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "textOnPlatformWithCRLF", attributes: "* text\n", platform: EOLCRLF,
			attr: CRLFText, effective: CRLFTextCRLF, checkout: ConvertCRLF, checkin: ConvertLF,
		},
		{
			name: "textWithEOLConfigured", attributes: "* text\n", eol: EOLCRLF, platform: EOLLF,
			attr: CRLFText, effective: CRLFTextCRLF, checkout: ConvertCRLF, checkin: ConvertLF,
		},
		{
			name: "textWithEOLLFConfigured", attributes: "* text\n", eol: EOLLF, platform: EOLCRLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "textWithNativeEOL", attributes: "* text\n", eol: EOLNative, platform: EOLCRLF,
			attr: CRLFText, effective: CRLFTextCRLF, checkout: ConvertCRLF, checkin: ConvertLF,
		},
		{
			name: "autoCRLFTrueOverridesEOL", attributes: "* text\n", autoCRLF: AutoCRLFTrue, eol: EOLLF, platform: EOLLF,
			attr: CRLFText, effective: CRLFTextCRLF, checkout: ConvertCRLF, checkin: ConvertLF,
		},
		{
			name: "autoCRLFInputOverridesEOL", attributes: "* text\n", autoCRLF: AutoCRLFInput, eol: EOLCRLF, platform: EOLCRLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "binaryAttribute", attributes: "* -text\n", autoCRLF: AutoCRLFTrue, platform: EOLCRLF,
			attr: CRLFBinary, effective: CRLFBinary, checkout: ConvertNone, checkin: ConvertNone,
		},
		{
			name: "binaryAttributeIgnoresEOL", attributes: "* -text eol=crlf\n", platform: EOLCRLF,
			attr: CRLFBinary, effective: CRLFBinary, checkout: ConvertNone, checkin: ConvertNone,
		},
		{
			name: "autoAttribute", attributes: "* text=auto\n", platform: EOLLF,
			attr: CRLFAuto, effective: CRLFAuto, checkout: ConvertLF, checkin: ConvertLF, detect: true,
		},
		{
			name: "autoAttributeOnCRLFPlatform", attributes: "* text=auto\n", platform: EOLCRLF,
			attr: CRLFAuto, effective: CRLFAuto, checkout: ConvertCRLF, checkin: ConvertLF, detect: true,
		},
		{
			name: "autoWithEOLLF", attributes: "* text=auto eol=lf\n", platform: EOLCRLF,
			attr: CRLFAutoInput, effective: CRLFAutoInput, checkout: ConvertLF, checkin: ConvertLF, detect: true,
		},
		{
			name: "autoWithEOLCRLF", attributes: "* text=auto eol=crlf\n", platform: EOLLF,
			attr: CRLFAutoCRLF, effective: CRLFAutoCRLF, checkout: ConvertCRLF, checkin: ConvertLF, detect: true,
		},
		{
			name: "eolAloneImpliesText", attributes: "* eol=lf\n", platform: EOLCRLF,
			attr: CRLFTextInput, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "eolCRLFAloneImpliesText", attributes: "* eol=crlf\n", platform: EOLLF,
			attr: CRLFTextCRLF, effective: CRLFTextCRLF, checkout: ConvertCRLF, checkin: ConvertLF,
		},
		{
			name: "unknownEOLValueIsIgnored", attributes: "* text eol=mac\n", platform: EOLLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "bareEOLIsIgnored", attributes: "* text eol\n", platform: EOLLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "legacyCRLFAttribute", attributes: "* crlf\n", platform: EOLLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "legacyCRLFAttributeUnset", attributes: "* -crlf\n", autoCRLF: AutoCRLFTrue, platform: EOLLF,
			attr: CRLFBinary, effective: CRLFBinary, checkout: ConvertNone, checkin: ConvertNone,
		},
		{
			name: "textAttributeBeatsLegacyCRLF", attributes: "* text -crlf\n", platform: EOLLF,
			attr: CRLFText, effective: CRLFTextInput, checkout: ConvertLF, checkin: ConvertLF,
		},
		{
			name: "unknownTextValueIsUndefined", attributes: "* text=maybe\n", platform: EOLLF,
			attr: CRLFUndefined, effective: CRLFBinary, checkout: ConvertNone, checkin: ConvertNone,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs := testAttributes(map[string]string{".gitattributes": tc.attributes}, AttributeOptions{
				AutoCRLF:    tc.autoCRLF,
				EOL:         tc.eol,
				PlatformEOL: tc.platform,
			})
			got := attrs.Text("a.txt")
			want := TextPolicy{
				Attr:      tc.attr,
				Effective: tc.effective,
				Convert:   Convert{OnCheckout: tc.checkout, OnCheckin: tc.checkin, Detect: tc.detect},
			}
			if got != want {
				t.Fatalf("Text() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestPlatformEndOfLineFallsBackToTheRunningSystem(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		want     bool
	}{
		{"explicitCRLF", EOLCRLF, true},
		{"explicitLF", EOLLF, false},
		{"nativeIsNotAPlatform", EOLNative, platformIsCRLF("")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := platformIsCRLF(tc.platform); got != tc.want {
				t.Fatalf("platformIsCRLF(%q) = %v, want %v", tc.platform, got, tc.want)
			}
		})
	}
}

func TestIsBinaryContentLooksForNulInTheFirstBytes(t *testing.T) {
	tests := []struct {
		name   string
		sample []byte
		want   bool
	}{
		{"empty", nil, false},
		{"text", []byte("alpha\nbeta\n"), false},
		{"nulAtTheStart", []byte("\x00abc"), true},
		{"nulInsideTheSample", append(bytes.Repeat([]byte("a"), 100), 0), true},
		{"nulBeyondTheSample", append(bytes.Repeat([]byte("a"), binarySampleSize), 0), false},
		{"nulAtTheSampleEdge", append(bytes.Repeat([]byte("a"), binarySampleSize-1), 0), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBinaryContent(tc.sample); got != tc.want {
				t.Fatalf("IsBinaryContent(%d bytes) = %v, want %v", len(tc.sample), got, tc.want)
			}
		})
	}
}

func TestDefaultConfigurationFilesFollowXDG(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		excludes   string
		attributes string
	}{
		{
			name:     "xdgWins",
			env:      map[string]string{"XDG_CONFIG_HOME": "/cfg", "HOME": "/home/user"},
			excludes: "/cfg/git/ignore", attributes: "/cfg/git/attributes",
		},
		{
			name:     "trailingSeparatorRemoved",
			env:      map[string]string{"XDG_CONFIG_HOME": "/cfg/"},
			excludes: "/cfg/git/ignore", attributes: "/cfg/git/attributes",
		},
		{
			name:     "homeFallback",
			env:      map[string]string{"HOME": "/home/user"},
			excludes: "/home/user/.config/git/ignore", attributes: "/home/user/.config/git/attributes",
		},
		{
			name:     "userProfileFallback",
			env:      map[string]string{"USERPROFILE": `C:\Users\u`},
			excludes: "C:/Users/u/.config/git/ignore", attributes: "C:/Users/u/.config/git/attributes",
		},
		{name: "nothingConfigured", env: map[string]string{}, excludes: "", attributes: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := func(key string) string { return tc.env[key] }
			if got := DefaultExcludesFile(env); got != tc.excludes {
				t.Fatalf("DefaultExcludesFile = %q, want %q", got, tc.excludes)
			}
			if got := DefaultAttributesFile(env); got != tc.attributes {
				t.Fatalf("DefaultAttributesFile = %q, want %q", got, tc.attributes)
			}
		})
	}
}

func TestDefaultExcludesFileReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/from/process")
	if got := DefaultExcludesFile(nil); got != "/from/process/git/ignore" {
		t.Fatalf("DefaultExcludesFile(nil) = %q, want %q", got, "/from/process/git/ignore")
	}
}
