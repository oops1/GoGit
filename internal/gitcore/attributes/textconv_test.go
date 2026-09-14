package attributes

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestGatherStatsCountsLineEndingsAndPrintableBytes(t *testing.T) {
	tests := []struct {
		name string
		data string
		want textStats
	}{
		{"empty", "", textStats{}},
		{"lineEndings", "a\r\nb\nc\rd", textStats{crlf: 1, loneLF: 1, loneCR: 1, printable: 4}},
		{"trailingCR", "a\r", textStats{loneCR: 1, printable: 1}},
		{"controlsCountedAsPrintable", "\b\t\x1b\x0c", textStats{printable: 4}},
		{"nulAndDelete", "\x00\x7f\x01", textStats{nul: 1, nonPrintable: 3}},
		{"endOfFileMarker", "ab\x1a", textStats{printable: 2}},
		{"highBytes", "\xc3\xa9", textStats{printable: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatherStats([]byte(tc.data)); got != tc.want {
				t.Fatalf("gatherStats(%q) = %+v, want %+v", tc.data, got, tc.want)
			}
		})
	}
}

func TestTextStatsTreatLoneCRNulAndControlsAsBinary(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"plain", "hello\r\nworld\n", false},
		{"loneCR", "a\rb", true},
		{"nul", "a\x00b", true},
		{"tooManyControls", strings.Repeat("a", 127) + "\x01", true},
		{"fewControls", strings.Repeat("a", 128) + "\x01", false},
		{"lateNul", strings.Repeat("a", 9000) + "\x00", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatherStats([]byte(tc.data)).binary(); got != tc.want {
				t.Fatalf("binary(%q) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestHasCRLFInIndexIgnoresBinaryAndLoneCR(t *testing.T) {
	tests := []struct {
		name string
		blob string
		want bool
	}{
		{"noCR", "a\nb\n", false},
		{"crlf", "a\r\nb\n", true},
		{"loneCROnly", "a\rb", false},
		{"binaryWithCRLF", "a\x00\r\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCRLFInIndex([]byte(tc.blob)); got != tc.want {
				t.Fatalf("hasCRLFInIndex(%q) = %v, want %v", tc.blob, got, tc.want)
			}
		})
	}
}

func policyFor(attrs, autoCRLF string) TextPolicy {
	return testAttributes(map[string]string{".gitattributes": attrs}, AttributeOptions{AutoCRLF: autoCRLF, PlatformEOL: EOLLF}).Text("f.txt")
}

func TestTextPolicyToGitFollowsCRLFToGit(t *testing.T) {
	crlfIndex := func() ([]byte, bool) { return []byte("x\r\n"), true }
	tests := []struct {
		name  string
		attrs string
		data  string
		index IndexBlob
		want  string
	}{
		{"binaryKeepsCRLF", "* -text\n", "a\r\n", nil, "a\r\n"},
		{"emptyStaysEmpty", "* text\n", "", nil, ""},
		{"noCRLF", "* text\n", "a\rb\n", nil, "a\rb\n"},
		{"textKeepsLoneCR", "* text\n", "a\rb\r\n", nil, "a\rb\n"},
		{"autoSkipsBinary", "* text=auto\n", "a\rb\r\n", nil, "a\rb\r\n"},
		{"autoKeepsCRLFOfTheIndex", "* text=auto\n", "a\r\n", crlfIndex, "a\r\n"},
		{"autoWithoutIndexEntry", "* text=auto\n", "a\r\n", func() ([]byte, bool) { return nil, false }, "a\n"},
		{"autoWithoutIndex", "* text=auto\n", "a\r\nb\r\n", nil, "a\nb\n"},
		{"textIgnoresTheIndex", "* text\n", "a\r\n", crlfIndex, "a\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := policyFor(tc.attrs, "").ToGit([]byte(tc.data), tc.index); string(got) != tc.want {
				t.Fatalf("ToGit(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestTextPolicyToWorkingTreeFollowsCRLFToWorktree(t *testing.T) {
	tests := []struct {
		name  string
		attrs string
		data  string
		want  string
	}{
		{"lfOutput", "* text eol=lf\n", "a\nb\n", "a\nb\n"},
		{"empty", "* text eol=crlf\n", "", ""},
		{"noLoneLF", "* text eol=crlf\n", "a\r\nb", "a\r\nb"},
		{"textConvertsMixedEndings", "* text eol=crlf\n", "a\r\nb\nc", "a\r\nb\r\nc"},
		{"textConvertsAroundLoneCR", "* text eol=crlf\n", "\na\rb\n", "\r\na\rb\r\n"},
		{"autoSkipsCRLF", "* text=auto eol=crlf\n", "a\r\nb\n", "a\r\nb\n"},
		{"autoSkipsLoneCR", "* text=auto eol=crlf\n", "a\rb\n", "a\rb\n"},
		{"autoSkipsBinary", "* text=auto eol=crlf\n", "a\x00\n", "a\x00\n"},
		{"autoConvertsText", "* text=auto eol=crlf\n", "a\nb\n", "a\r\nb\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := policyFor(tc.attrs, "").ToWorkingTree([]byte(tc.data)); string(got) != tc.want {
				t.Fatalf("ToWorkingTree(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestCRLFInputAttributeNormalizesWithoutCheckoutConversion(t *testing.T) {
	policy := policyFor("* crlf=input\n", AutoCRLFTrue)
	if policy.Attr != CRLFTextInput || policy.Convert.OnCheckout != ConvertLF {
		t.Fatalf("crlf=input gives %+v", policy)
	}
}

const identID = "0123456789012345678901234567890123456789"

func TestIdentConversionsFollowGit(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		toGit    string
		worktree string
	}{
		{"noKeyword", "plain $ text", "plain $ text", "plain $ text"},
		{"tooShort", "$Id", "$Id", "$Id"},
		{"notTheKeyword", "$Ix$ $Idx", "$Ix$ $Idx", "$Ix$ $Idx"},
		{"collapsed", "a $Id$ b", "a $Id$ b", "a $Id: " + identID + " $ b"},
		{"expanded", "$Id: abc $\n", "$Id$\n", "$Id: " + identID + " $\n"},
		{"expandedEmpty", "$Id:$", "$Id$", "$Id: " + identID + " $"},
		{"expandedWithoutSpace", "$Id:x$", "$Id$", "$Id: " + identID + " $"},
		{"foreignKeyword", "$Id: foo bar $", "$Id$", "$Id: foo bar $"},
		{"lineBreak", "$Id: a\nb $ $Id$", "$Id: a\nb $ $Id$", "$Id: a\nb $ $Id: " + identID + " $"},
		{"unterminated", "$Id$ $Id: never", "$Id$ $Id: never", "$Id: " + identID + " $ $Id: never"},
		{"unterminatedAtEnd", "$Id$ $Id:", "$Id$ $Id:", "$Id: " + identID + " $ $Id:"},
		{"unclosedLine", "$Id: a\n", "$Id: a\n", "$Id: a\n"},
		{"otherKeywordAfterIdent", "$Id$ $Idx y", "$Id$ $Idx y", "$Id: " + identID + " $ $Idx y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := identToGit([]byte(tc.data)); string(got) != tc.toGit {
				t.Errorf("identToGit(%q) = %q, want %q", tc.data, got, tc.toGit)
			}
			if got := identToWorkingTree([]byte(tc.data), identID); string(got) != tc.worktree {
				t.Errorf("identToWorkingTree(%q) = %q, want %q", tc.data, got, tc.worktree)
			}
		})
	}
}

func lfsPointerText(oid string, size string) string {
	return "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize " + size + "\n"
}

var lfsTestOID = strings.Repeat("ab", 32)

func TestLFSPointerRecognitionFollowsGitLFS(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"canonical", lfsPointerText(lfsTestOID, "12"), true},
		{"legacyVersion", "version https://hawser.github.com/spec/v1\noid sha256:" + lfsTestOID + "\nsize 1", true},
		{"blankLines", "version http://git-media.io/v/2\n\noid sha256:" + lfsTestOID + "\r\nsize 0", true},
		{"extension", "version " + lfsSpecURL + "\next-0-foo sha256:" + lfsTestOID + "\noid sha256:" + lfsTestOID + "\nsize 3", true},
		{"badExtension", "version " + lfsSpecURL + "\next-0-foo md5:" + lfsTestOID + "\noid sha256:" + lfsTestOID + "\nsize 3", false},
		{"unknownKey", "version " + lfsSpecURL + "\nname x\noid sha256:" + lfsTestOID + "\nsize 3", false},
		{"missingValue", "version\noid sha256:" + lfsTestOID + "\nsize 3", false},
		{"extraLine", lfsPointerText(lfsTestOID, "3") + "more data", false},
		{"unknownVersion", "version https://example.com\noid sha256:" + lfsTestOID + "\nsize 3", false},
		{"shortOID", lfsPointerText("abc", "3"), false},
		{"otherHash", "version " + lfsSpecURL + "\noid md5:" + lfsTestOID + "\nsize 3", false},
		{"negativeSize", lfsPointerText(lfsTestOID, "-1"), false},
		{"missingSize", "version " + lfsSpecURL + "\noid sha256:" + lfsTestOID, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLFSPointer([]byte(tc.data)); got != tc.want {
				t.Fatalf("isLFSPointer(%q) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestLFSCleanPassesPointersThroughAndHashesEverythingElse(t *testing.T) {
	pointer := lfsPointerText(lfsTestOID, "5")
	large := "\n" + pointer + strings.Repeat(" ", lfsPassThroughSize)
	content := "binary\x00content"
	tests := []struct {
		name    string
		data    string
		want    string
		through bool
	}{
		{"empty", "", "", true},
		{"pointer", pointer, pointer, true},
		{"paddedPointer", " \n" + pointer + "\n\n", " \n" + pointer + "\n\n", true},
		{"pointerTooLarge", large, string(lfsPointer(sha256.Sum256([]byte(large)), int64(len(large)))), false},
		{"content", content, string(lfsPointer(sha256.Sum256([]byte(content)), int64(len(content)))), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, through := lfsClean([]byte(tc.data))
			if string(got) != tc.want || through != tc.through {
				t.Fatalf("lfsClean(%.40q) = %.80q, %v, want %.80q, %v", tc.data, got, through, tc.want, tc.through)
			}
		})
	}
	sum := sha256.Sum256([]byte("hi"))
	if got, want := string(lfsPointer(sum, 2)), lfsPointerText(strings.ToLower(hexOf(sum[:])), "2"); got != want {
		t.Fatalf("lfsPointer = %q, want %q", got, want)
	}
}

func hexOf(data []byte) string {
	const digits = "0123456789abcdef"
	var out strings.Builder
	for _, b := range data {
		out.WriteByte(digits[b>>4])
		out.WriteByte(digits[b&0x0f])
	}
	return out.String()
}

func TestEncodingNameSkipsUTF8AndRejectsBooleans(t *testing.T) {
	tests := []struct {
		name  string
		value Value
		want  string
		err   error
	}{
		{"unspecified", UnspecifiedValue(), "", nil},
		{"set", SetValue(), "", ErrInvalidEncoding},
		{"unset", UnsetValue(), "", ErrInvalidEncoding},
		{"empty", TextValue(""), "", nil},
		{"utf8", TextValue("utf8"), "", nil},
		{"UTF-8", TextValue("UTF-8"), "", nil},
		{"utf16", TextValue("UTF-16"), "UTF-16", nil},
		{"shortName", TextValue("ut"), "ut", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodingName(tc.value)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("encodingName(%v) = %q, %v, want %q, %v", tc.value, got, err, tc.want, tc.err)
			}
		})
	}
}

func TestValidateEncodingChecksByteOrderMarks(t *testing.T) {
	tests := []struct {
		name string
		enc  string
		data string
		ok   bool
	}{
		{"notUTF", "ISO-8859-1", "\xfe\xff", true},
		{"utf16leWithBOM", "UTF-16LE", "\xff\xfea\x00", false},
		{"utf16beWithBOM", "utf16be", "\xfe\xff\x00a", false},
		{"utf16leWithoutBOM", "UTF-16LE", "a\x00", true},
		{"utf32beWithBOM", "UTF-32BE", "\x00\x00\xfe\xff", false},
		{"utf32leWithoutBOM", "UTF-32LE", "a\x00\x00\x00", true},
		{"utf16MissingBOM", "UTF-16", "\x00a", false},
		{"utf16WithBOM", "UTF-16", "\xff\xfea\x00", true},
		{"utf32MissingBOM", "UTF-32", "\x00\x00\x00a", false},
		{"utf32WithBOM", "UTF-32", "\xff\xfe\x00\x00a\x00\x00\x00", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEncoding(tc.enc, []byte(tc.data))
			if (err == nil) != tc.ok || err != nil && !errors.Is(err, ErrEncodingBOM) {
				t.Fatalf("validateEncoding(%q, %q) = %v, want ok %v", tc.enc, tc.data, err, tc.ok)
			}
		})
	}
}

func TestDecodeToUTF8FollowsIconv(t *testing.T) {
	tests := []struct {
		name string
		enc  string
		data string
		want string
		ok   bool
	}{
		{"utf16BigEndianBOM", "UTF-16", "\xfe\xff\x00a", "a", true},
		{"utf16LittleEndianBOM", "UTF-16", "\xff\xfea\x00", "a", true},
		{"utf16SurrogatePair", "UTF-16BE", "\xd8\x3d\xde\x00", "\U0001f600", true},
		{"utf16OddLength", "UTF-16BE", "\x00a\x00", "", false},
		{"utf16HighAtEnd", "UTF-16BE", "\xd8\x3d", "", false},
		{"utf16HighThenText", "UTF-16BE", "\xd8\x3d\x00a", "", false},
		{"utf16HighThenHigh", "UTF-16BE", "\xd8\x3d\xd8\x3d", "", false},
		{"utf16LoneLow", "UTF-16LE", "\x00\xde", "", false},
		{"utf32BOM", "UTF-32", "\xff\xfe\x00\x00a\x00\x00\x00", "a", true},
		{"utf32BigEndian", "utf-32be", "\x00\x01\xf6\x00", "\U0001f600", true},
		{"utf32TooLarge", "UTF-32LE", "\x00\x00\x11\x00", "", false},
		{"utf32Negative", "UTF-32LE", "\xff\xff\xff\xff", "", false},
		{"utf32Surrogate", "UTF-32BE", "\x00\x00\xd8\x00", "", false},
		{"utf16BEWithBOMSuffixIsUnknownForReading", "UTF-16BE-BOM", "\xfe\xff\x00a", "", false},
		{"latin1", "ISO-8859-1", "caf\xe9", "caf\xc3\xa9", true},
		{"latin1Alias", "latin-1", "caf\xe9", "caf\xc3\xa9", true},
		{"unknownName", "no-such-encoding", "a", "", false},
		{"unsupportedEncoding", "UTF-7", "a", "", false},
		{"invalidShiftJIS", "Shift_JIS", "\x82", "", false},
		{"shiftJIS", "Shift_JIS", "\x82\xa0", "\xe3\x81\x82", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := decodeToUTF8(tc.enc, []byte(tc.data))
			if ok != tc.ok || string(got) != tc.want {
				t.Fatalf("decodeToUTF8(%q, %q) = %q, %v, want %q, %v", tc.enc, tc.data, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestEncodeFromUTF8FollowsIconv(t *testing.T) {
	tests := []struct {
		name string
		enc  string
		data string
		want string
		ok   bool
	}{
		{"utf16LEBOM", "utf16le-bom", "a", "\xff\xfea\x00", true},
		{"utf16BEBOM", "UTF-16BE-BOM", "a", "\xfe\xff\x00a", true},
		{"utf16LESurrogates", "UTF-16LE", "\U0001f600", "\x3d\xd8\x00\xde", true},
		{"utf32LE", "UTF-32LE", "a", "a\x00\x00\x00", true},
		{"utf32BE", "UTF-32BE", "a", "\x00\x00\x00a", true},
		{"invalidUTF8", "UTF-16", "\xc3", "", false},
		{"latin1", "ISO-8859-1", "caf\xc3\xa9", "caf\xe9", true},
		{"latin1CannotHoldEuro", "ISO-8859-1", "\xe2\x82\xac", "", false},
		{"unknownName", "no-such-encoding", "a", "", false},
		{"unsupportedEncoding", "UTF-7", "a", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := encodeFromUTF8(tc.enc, []byte(tc.data))
			if ok != tc.ok || string(got) != tc.want {
				t.Fatalf("encodeFromUTF8(%q, %q) = %q, %v, want %q, %v", tc.enc, tc.data, got, ok, tc.want, tc.ok)
			}
		})
	}
}

type iconvCase struct {
	name string
	enc  string
	data string
	want string
	ok   bool
}

func checkIconvCases(t *testing.T, convert func(string, []byte) ([]byte, bool), cases []iconvCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := convert(tc.enc, []byte(tc.data))
			if ok != tc.ok || string(got) != tc.want {
				t.Fatalf("convert(%q, %q) = %q, %v, want %q, %v", tc.enc, tc.data, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func loadConfig(t *testing.T, text string) *config.Config {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte(text), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	cfg, err := config.Load(config.Options{GlobalFile: global, NoSystem: true})
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	return cfg
}

const lfsDriverConfig = "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge -- %f\n\tprocess = git-lfs filter-process\n\trequired = true\n"

func TestPolicyClassifiesFilterDrivers(t *testing.T) {
	attrs := "*.lfs filter=lfs\n*.ext filter=ext\n*.empty filter=empty\n*.none filter=unknown\n*.clean filter=cleanonly\n*.plain -filter\n"
	tests := []struct {
		name   string
		config string
		path   string
		want   FilterKind
	}{
		{"noConfig", "", "a.lfs", FilterNone},
		{"lfsProcess", lfsDriverConfig, "a.lfs", FilterLFS},
		{"lfsCleanOnly", "[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n", "a.lfs", FilterLFS},
		{"lfsExtension", lfsDriverConfig + "[lfs \"extension.foo\"]\n\tclean = foo\n", "a.lfs", FilterExternal},
		{"lfsOtherSubsection", lfsDriverConfig + "[lfs \"https://example.com\"]\n\taccess = basic\n", "a.lfs", FilterLFS},
		{"externalCommand", "[filter \"ext\"]\n\tclean = indent\n", "a.ext", FilterExternal},
		{"emptyProcessHidesClean", "[filter \"empty\"]\n\tprocess =\n\tclean = indent\n", "a.empty", FilterNone},
		{"smudgeOnly", "[filter \"cleanonly\"]\n\tsmudge = indent\n", "a.clean", FilterNone},
		{"undefinedDriver", lfsDriverConfig, "a.none", FilterNone},
		{"unsetAttribute", lfsDriverConfig, "a.plain", FilterNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := AttributeOptions{}
			if tc.config != "" {
				opts.Config = loadConfig(t, tc.config)
			}
			if got := testAttributes(map[string]string{".gitattributes": attrs}, opts).Policy(tc.path).Clean; got != tc.want {
				t.Fatalf("Policy(%q).Clean = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

func TestPolicyToGitRefusesFiltersItCannotRun(t *testing.T) {
	cfg := loadConfig(t, lfsDriverConfig+"[filter \"ext\"]\n\tclean = indent\n")
	attrs := testAttributes(map[string]string{".gitattributes": "*.lfs filter=lfs\n*.ext filter=ext\n"}, AttributeOptions{Config: cfg})
	content := []byte("large binary\x00content")
	pointer, _ := lfsClean(content)
	holds := func(blob []byte) IndexBlob { return func() ([]byte, bool) { return blob, true } }

	if _, err := attrs.Policy("a.ext").ToGit(content, nil); !errors.Is(err, ErrFilterUnsupported) || !strings.Contains(err.Error(), "a.ext") {
		t.Fatalf("ToGit of an external filter returned %v", err)
	}
	if got := attrs.Policy("a.ext").CompareToGit(content, nil); !bytes.Equal(got, content) {
		t.Fatalf("CompareToGit of an external filter = %q", got)
	}
	if _, err := attrs.Policy("a.lfs").ToGit(content, nil); !errors.Is(err, ErrFilterUnsupported) {
		t.Fatalf("ToGit of new LFS content returned %v", err)
	}
	if _, err := attrs.Policy("a.lfs").ToGit(content, holds([]byte("other"))); !errors.Is(err, ErrFilterUnsupported) {
		t.Fatalf("ToGit of changed LFS content returned %v", err)
	}
	if got, err := attrs.Policy("a.lfs").ToGit(content, holds(pointer)); err != nil || !bytes.Equal(got, pointer) {
		t.Fatalf("ToGit of unchanged LFS content = %q, %v", got, err)
	}
	if got, err := attrs.Policy("a.lfs").ToGit(pointer, nil); err != nil || !bytes.Equal(got, pointer) {
		t.Fatalf("ToGit of an LFS pointer = %q, %v", got, err)
	}
	if got := attrs.Policy("a.lfs").CompareToGit(content, nil); !bytes.Equal(got, pointer) {
		t.Fatalf("CompareToGit of LFS content = %q, want %q", got, pointer)
	}
}

func TestPolicyToGitReportsEncodingProblemsOnlyWhenStrict(t *testing.T) {
	attrs := testAttributes(map[string]string{".gitattributes": "*.bool working-tree-encoding\n*.u16 working-tree-encoding=UTF-16\n*.bad working-tree-encoding=no-such\n"}, AttributeOptions{})
	data := []byte("\x00a")
	tests := []struct {
		path string
		err  error
	}{
		{"a.bool", ErrInvalidEncoding},
		{"a.u16", ErrEncodingBOM},
		{"a.bad", ErrEncodingFailed},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			policy := attrs.Policy(tc.path)
			if _, err := policy.ToGit(data, nil); !errors.Is(err, tc.err) || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("ToGit returned %v, want %v", err, tc.err)
			}
			if got := policy.CompareToGit(data, nil); !bytes.Equal(got, data) {
				t.Fatalf("CompareToGit = %q, want the content unchanged", got)
			}
			if got := policy.ToWorkingTree([]byte("\xc3")); string(got) != "\xc3" {
				t.Fatalf("ToWorkingTree = %q, want the content unchanged", got)
			}
		})
	}
	if got, err := attrs.Policy("a.u16").ToGit(nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("ToGit of empty content = %q, %v", got, err)
	}
	if got := attrs.Policy("a.u16").ToWorkingTree(nil); len(got) != 0 {
		t.Fatalf("ToWorkingTree of empty content = %q", got)
	}
}

func TestPolicyRunsTheConversionsInGitOrder(t *testing.T) {
	attrs := testAttributes(map[string]string{".gitattributes": "* ident text eol=crlf working-tree-encoding=UTF-16LE-BOM\n"}, AttributeOptions{})
	policy := attrs.Policy("a.txt")
	blob := []byte("$Id$\nline\n")
	id, err := hash.Sum(hash.SHA1, "blob", blob)
	if err != nil {
		t.Fatalf("Sum returned error %v", err)
	}
	worktree := policy.ToWorkingTree(blob)
	want, _ := encodeFromUTF8("UTF-16LE-BOM", []byte("$Id: "+id.String()+" $\r\nline\r\n"))
	if !bytes.Equal(worktree, want) {
		t.Fatalf("ToWorkingTree = %q, want %q", worktree, want)
	}
	back, err := policy.ToGit(worktree, nil)
	if err != nil || !bytes.Equal(back, blob) {
		t.Fatalf("ToGit = %q, %v, want %q", back, err, blob)
	}
}

func TestIdentOfAnUnsupportedObjectFormatIsLeftAlone(t *testing.T) {
	attrs := testAttributes(map[string]string{".gitattributes": "* ident\n"}, AttributeOptions{ObjectFormat: hash.SHA256})
	if got := attrs.Policy("a").ToWorkingTree([]byte("$Id$")); string(got) != "$Id$" {
		t.Fatalf("ToWorkingTree = %q, want the keyword untouched", got)
	}
}
