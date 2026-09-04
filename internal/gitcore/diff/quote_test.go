package diff

import "testing"

func TestQuoteCStyleEscapesLikeGit(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a plain name is left alone", "dir/file.txt", "dir/file.txt"},
		{"a space needs no quoting", "with space.txt", "with space.txt"},
		{"a bell is escaped", "a\ab", `"a\ab"`},
		{"a backspace is escaped", "a\bb", `"a\bb"`},
		{"a tab is escaped", "a\tb", `"a\tb"`},
		{"a newline is escaped", "a\nb", `"a\nb"`},
		{"a vertical tab is escaped", "a\vb", `"a\vb"`},
		{"a form feed is escaped", "a\fb", `"a\fb"`},
		{"a carriage return is escaped", "a\rb", `"a\rb"`},
		{"a quote is escaped", `a"b`, `"a\"b"`},
		{"a backslash is escaped", `a\b`, `"a\\b"`},
		{"a control byte becomes an octal escape", "a\x01b", `"a\001b"`},
		{"a high byte becomes an octal escape", "\xd0\xbf", `"\320\277"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteCStyle(c.in); got != c.want {
				t.Errorf("quoteCStyle(%q) returned %s instead of %s", c.in, got, c.want)
			}
		})
	}
}

func TestQuoteTwoJoinsThePrefixInsideTheQuotes(t *testing.T) {
	if got := quoteTwo("a/", "file.txt"); got != "a/file.txt" {
		t.Errorf("quoteTwo returned %s instead of a/file.txt", got)
	}
	if got := quoteTwo("a/", "od\nd"); got != `"a/od\nd"` {
		t.Errorf("quoteTwo returned %s instead of a quoted name", got)
	}
	if got := quoteTwo("a\n/", "file"); got != `"a\n/file"` {
		t.Errorf("quoteTwo returned %s instead of a quoted name", got)
	}
}

func TestRenameNameFoldsTheCommonPathParts(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{"no shared part", "one.txt", "two.txt", "one.txt => two.txt"},
		{"shared directory", "dir/one.txt", "dir/two.txt", "dir/{one.txt => two.txt}"},
		{"shared basename", "one/report.txt", "two/report.txt", "{one => two}/report.txt"},
		{"moved into a directory", "top.txt", "sub/top.txt", "top.txt => sub/top.txt"},
		{"moved out of a directory", "sub/inner.txt", "inner.txt", "sub/inner.txt => inner.txt"},
		{"shared trailing directory", "old/deep/x.txt", "new/deep/x.txt", "{old => new}/deep/x.txt"},
		{"prefix and suffix", "a/b/one/x.txt", "a/b/two/x.txt", "a/b/{one => two}/x.txt"},
		{"a quoted side is spelled out", "one.txt", "od\nd.txt", `one.txt => "od\nd.txt"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renameName(c.old, c.new); got != c.want {
				t.Errorf("renameName(%q, %q) returned %s instead of %s", c.old, c.new, got, c.want)
			}
		})
	}
}

func TestByteAtReportsZeroPastTheEnd(t *testing.T) {
	if got := byteAt("ab", 1); got != 'b' {
		t.Errorf("byteAt returned %q instead of 'b'", got)
	}
	if got := byteAt("ab", 2); got != 0 {
		t.Errorf("byteAt past the end returned %q instead of zero", got)
	}
}
