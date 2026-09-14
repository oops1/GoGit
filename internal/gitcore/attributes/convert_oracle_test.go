//go:build oracle

package attributes

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type conversionAttr struct {
	name  string
	attrs string
}

var conversionAttrs = []conversionAttr{
	{"none", ""},
	{"text", "text"},
	{"binary", "-text"},
	{"auto", "text=auto"},
	{"textcrlf", "text eol=crlf"},
	{"textlf", "text eol=lf"},
	{"autocrlf", "text=auto eol=crlf"},
	{"autolf", "text=auto eol=lf"},
	{"crlfinput", "crlf=input"},
	{"ident", "ident"},
	{"identcrlf", "ident text eol=crlf"},
	{"encutf16", "working-tree-encoding=UTF-16"},
	{"encutf16lebom", "text eol=crlf working-tree-encoding=UTF-16LE-BOM"},
	{"encutf16le", "working-tree-encoding=utf16le"},
	{"encutf32", "working-tree-encoding=UTF-32"},
	{"enclatin", "working-tree-encoding=ISO-8859-1"},
	{"encshiftjis", "working-tree-encoding=Shift_JIS"},
	{"encunknown", "working-tree-encoding=no-such-encoding"},
	{"encutf8", "working-tree-encoding=utf8"},
}

var conversionContents = []struct {
	name string
	data string
}{
	{"empty", ""},
	{"lf", "alpha\nbeta\n"},
	{"crlf", "alpha\r\nbeta\r\n"},
	{"mixed", "alpha\r\nbeta\ngamma\n"},
	{"lonecr", "alpha\rbeta\r\ngamma\n"},
	{"trailingcr", "alpha\r\nbeta\r"},
	{"nul", "alpha\x00\r\nbeta\n"},
	{"latenul", strings.Repeat("line\r\n", 2000) + "tail\n\x00"},
	{"controls", "\x01\x02\x03\x04\r\nab\n"},
	{"fewcontrols", strings.Repeat("printable text ", 10) + "\x01\r\nend\n"},
	{"eofmark", strings.Repeat("printable ", 12) + "\x01\r\nx\n\x1a"},
	{"ident", "$Id$\r\nnext\n"},
	{"identexpanded", "a $Id: 0123 $ b\n$Id: foo bar $\n$Id:$\n$Id: x\ny $\n$Id$Id$\n$Id"},
	{"utf16bom", "\xfe\xff\x00a\x00\r\x00\n\x00b\x00\n"},
	{"utf16lebom", "\xff\xfea\x00\r\x00\n\x00"},
	{"utf16nobom", "\x00a\x00\n"},
	{"utf16odd", "\xfe\xff\x00a\x00"},
	{"utf16pair", "\xfe\xff\xd8\x3d\xde\x00\x00\n"},
	{"utf16lonesurrogate", "\xfe\xff\xd8\x3d\x00\n"},
	{"utf32bom", "\x00\x00\xfe\xff\x00\x00\x00a\x00\x00\x00\n"},
	{"latin", "caf\xe9\r\n"},
	{"sjis", "\x82\xa0\r\n"},
	{"utf8", "caf\xc3\xa9 \xe2\x82\xac\n"},
	{"badutf8", "caf\xe9 \xc3\n"},
}

const crlfIndexBlob = "old\r\nline\r\n"

func conversionFixture(o *oracle) []string {
	var attrs strings.Builder
	var names []string
	for _, attr := range conversionAttrs {
		if attr.attrs != "" {
			fmt.Fprintf(&attrs, "%s-* %s\n", attr.name, attr.attrs)
		}
		for _, content := range conversionContents {
			if attr.name == "encutf16lebom" && content.name == "utf32bom" {
				continue
			}
			names = append(names, attr.name+"-"+content.name)
		}
	}
	o.write(".gitattributes", attrs.String())
	return names
}

func conversionContent(name string) []byte {
	_, contentName, _ := strings.Cut(name, "-")
	for _, content := range conversionContents {
		if content.name == contentName {
			return []byte(content.data)
		}
	}
	panic("unknown content " + contentName)
}

func (o *oracle) writeContents(names []string) {
	o.t.Helper()
	for _, name := range names {
		o.write(name, string(conversionContent(name)))
	}
}

func (o *oracle) conversionAttributes(autoCRLF string) *Attributes {
	o.t.Helper()
	root, err := os.OpenRoot(o.dir)
	if err != nil {
		o.t.Fatalf("OpenRoot returned error %v", err)
	}
	o.t.Cleanup(func() { _ = root.Close() })
	return New(AttributeOptions{Work: RootLoader(root), AutoCRLF: autoCRLF})
}

func (o *oracle) hashPaths(args []string, names []string) map[string]string {
	o.t.Helper()
	out, err := o.command([]byte(strings.Join(names, "\n")+"\n"), append(args, "--stdin-paths")...)
	if err != nil {
		o.t.Fatalf("%v", err)
	}
	ids := strings.Fields(out)
	if len(ids) != len(names) {
		o.t.Fatalf("hash-object printed %d ids for %d paths", len(ids), len(names))
	}
	result := make(map[string]string, len(names))
	for at, name := range names {
		result[name] = ids[at]
	}
	return result
}

func blobID(t *testing.T, data []byte) string {
	t.Helper()
	id, err := hash.Sum(hash.SHA1, "blob", data)
	if err != nil {
		t.Fatalf("Sum returned error %v", err)
	}
	return id.String()
}

func encodingCase(name string) bool {
	return strings.HasPrefix(name, "enc")
}

func TestConversionToGitMatchesGitHashObject(t *testing.T) {
	for _, autoCRLF := range []string{AutoCRLFFalse, AutoCRLFTrue, AutoCRLFInput} {
		t.Run("autocrlf="+autoCRLF, func(t *testing.T) {
			o := newOracle(t)
			o.git("config", "core.autocrlf", autoCRLF)
			names := conversionFixture(o)
			o.writeContents(names)
			attrs := o.conversionAttributes(autoCRLF)
			want := o.hashPaths([]string{"hash-object"}, names)
			for _, name := range names {
				got := attrs.Policy(name).CompareToGit(conversionContent(name), nil)
				if id := blobID(t, got); id != want[name] {
					t.Errorf("%s hashes to %s after conversion (%.60q), git says %s", name, id, got, want[name])
				}
			}
		})
	}
}

func TestStrictConversionFailsWhereGitRefusesToWriteTheObject(t *testing.T) {
	o := newOracle(t)
	names := conversionFixture(o)
	o.writeContents(names)
	attrs := o.conversionAttributes(AutoCRLFFalse)
	for _, name := range names {
		if !encodingCase(name) {
			continue
		}
		out, gitErr := o.command(nil, "hash-object", "-w", "--", name)
		got, err := attrs.Policy(name).ToGit(conversionContent(name), nil)
		switch {
		case (err != nil) != (gitErr != nil):
			t.Errorf("%s: our error %v, git error %v", name, err, gitErr)
		case err == nil && blobID(t, got) != strings.TrimSpace(out):
			t.Errorf("%s: we store %.60q, git stores %s", name, got, out)
		}
	}
}

func TestConversionToGitKeepsCRLFTheIndexAlreadyHolds(t *testing.T) {
	for _, autoCRLF := range []string{AutoCRLFFalse, AutoCRLFTrue} {
		t.Run("autocrlf="+autoCRLF, func(t *testing.T) {
			o := newOracle(t)
			o.git("config", "core.autocrlf", autoCRLF)
			names := slices.DeleteFunc(conversionFixture(o), encodingCase)
			blob := strings.TrimSpace(mustCommand(o, []byte(crlfIndexBlob), "hash-object", "-w", "--no-filters", "--stdin"))
			var info strings.Builder
			for _, name := range names {
				fmt.Fprintf(&info, "100644 %s\t%s\n", blob, name)
			}
			mustCommand(o, []byte(info.String()), "update-index", "--index-info")
			o.writeContents(names)
			o.git(append([]string{"add", "--"}, names...)...)
			staged := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimSpace(o.git("ls-files", "-s")), "\n") {
				info, path, _ := strings.Cut(line, "\t")
				staged[path] = strings.Fields(info)[1]
			}
			attrs := o.conversionAttributes(autoCRLF)
			index := func() ([]byte, bool) { return []byte(crlfIndexBlob), true }
			for _, name := range names {
				got, err := attrs.Policy(name).ToGit(conversionContent(name), index)
				if err != nil {
					t.Fatalf("ToGit(%s) returned error %v", name, err)
				}
				if id := blobID(t, got); id != staged[name] {
					t.Errorf("%s is staged as %s by git, we produce %s (%.60q)", name, staged[name], id, got)
				}
			}
		})
	}
}

func mustCommand(o *oracle, stdin []byte, args ...string) string {
	o.t.Helper()
	out, err := o.command(stdin, args...)
	if err != nil {
		o.t.Fatalf("%v", err)
	}
	return out
}

func TestConversionToWorkingTreeMatchesGitCheckout(t *testing.T) {
	for _, autoCRLF := range []string{AutoCRLFFalse, AutoCRLFTrue, AutoCRLFInput} {
		t.Run("autocrlf="+autoCRLF, func(t *testing.T) {
			o := newOracle(t)
			o.git("config", "core.autocrlf", autoCRLF)
			names := conversionFixture(o)
			o.writeContents(names)
			ids := o.hashPaths([]string{"hash-object", "-w", "--no-filters"}, names)
			var info strings.Builder
			for _, name := range names {
				fmt.Fprintf(&info, "100644 %s\t%s\n", ids[name], name)
				if err := os.Remove(o.dir + "/" + name); err != nil {
					t.Fatalf("Remove returned error %v", err)
				}
			}
			mustCommand(o, []byte(info.String()), "update-index", "--add", "--index-info")
			_, _ = o.command(nil, "checkout-index", "-f", "-a")
			attrs := o.conversionAttributes(autoCRLF)
			for _, name := range names {
				got := attrs.Policy(name).ToWorkingTree(conversionContent(name))
				if want := o.read(name); !bytes.Equal(got, want) {
					t.Errorf("%s is checked out as %.60q by git, we write %.60q", name, want, got)
				}
			}
		})
	}
}
