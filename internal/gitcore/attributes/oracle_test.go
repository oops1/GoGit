//go:build oracle

package attributes

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type oracle struct {
	t   *testing.T
	dir string
	env []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	for _, dir := range []string{home, work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) returned error %v", dir, err)
		}
	}
	o := &oracle{
		t:   t,
		dir: work,
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
		},
	}
	o.git("init", "-q", ".")
	return o
}

func (o *oracle) command(stdin []byte, args ...string) (string, error) {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = o.dir
	cmd.Env = o.env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, errBuf.String())
	}
	return out.String(), err
}

func (o *oracle) git(args ...string) string {
	o.t.Helper()
	out, err := o.command(nil, args...)
	if err != nil {
		o.t.Fatalf("%v", err)
	}
	return out
}

func (o *oracle) write(rel, text string) {
	o.t.Helper()
	full := filepath.Join(o.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		o.t.Fatalf("MkdirAll(%q) returned error %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(text), 0o600); err != nil {
		o.t.Fatalf("WriteFile(%q) returned error %v", full, err)
	}
}

func (o *oracle) mkdir(rel string) {
	o.t.Helper()
	if err := os.MkdirAll(filepath.Join(o.dir, filepath.FromSlash(strings.TrimSuffix(rel, "/"))), 0o700); err != nil {
		o.t.Fatalf("MkdirAll(%q) returned error %v", rel, err)
	}
}

func (o *oracle) read(rel string) []byte {
	o.t.Helper()
	data, err := os.ReadFile(filepath.Join(o.dir, filepath.FromSlash(rel)))
	if err != nil {
		o.t.Fatalf("ReadFile(%q) returned error %v", rel, err)
	}
	return data
}

func (o *oracle) tree() []Path {
	o.t.Helper()
	var out []Path
	root := os.DirFS(o.dir)
	err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if name == ".git" {
			return fs.SkipDir
		}
		out = append(out, Path{Name: name, IsDir: entry.IsDir()})
		return nil
	})
	if err != nil {
		o.t.Fatalf("WalkDir returned error %v", err)
	}
	return out
}

func nulJoin(items []string) []byte {
	var b bytes.Buffer
	for _, item := range items {
		b.WriteString(item)
		b.WriteByte(0)
	}
	return b.Bytes()
}

func nulFields(text string) []string {
	fields := strings.Split(text, "\x00")
	if len(fields) > 0 && fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	return fields
}

func (o *oracle) checkIgnore(paths []string) map[string]string {
	o.t.Helper()
	out, err := o.command(nulJoin(paths), "check-ignore", "-v", "-n", "-z", "--no-index", "--stdin")
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		o.t.Fatalf("%v", err)
	}
	fields := nulFields(out)
	if len(fields)%4 != 0 {
		o.t.Fatalf("check-ignore produced %d fields, want a multiple of four", len(fields))
	}
	result := map[string]string{}
	for i := 0; i < len(fields); i += 4 {
		source, line, pattern, path := fields[i], fields[i+1], fields[i+2], fields[i+3]
		if source == "" {
			result[path] = ""
			continue
		}
		result[path] = source + ":" + line + ":" + pattern
	}
	return result
}

func ignoreFixture(o *oracle) {
	o.write(".gitignore", strings.Join([]string{
		"# a comment",
		"",
		"*.log",
		"!important.log",
		"build/",
		"/rootonly.txt",
		"doc/**/draft.txt",
		"**/tmp",
		`space\ file.txt`,
		"trailing   ",
		`\#hash.txt`,
		`\!bang.txt`,
		"a**b.txt",
		"[Cc]ache",
		"?ne.txt",
		"sub/mid.txt",
	}, "\n")+"\n")
	o.write("sub/.gitignore", "keep.txt\n!keep.txt\nnested/\n/anchored.txt\n!*.log\n")
	o.write("sub/deep/.gitignore", "*.tmp\n!special.tmp\n")
	o.write("build/.gitignore", "!kept.me\n")
	o.write(".git/info/exclude", "info-only.txt\n*.excl\n")
	o.write("gitignore-global", "global-only.txt\n*.glb\n")

	for _, name := range []string{
		"a.log", "important.log", "rootonly.txt", "trailing", "#hash.txt", "!bang.txt",
		"space file.txt", "axxb.txt", "ab.txt", "Cache", "one.txt", "none.txt",
		"info-only.txt", "keep.excl", "global-only.txt", "keep.glb", "plain.txt",
		"MixedCase.LOG",
		"build/kept.me", "build/other.txt",
		"doc/draft.txt", "doc/x/draft.txt", "doc/x/y/draft.txt", "doc/x/final.txt",
		"tmp/inside.txt", "nested/tmp/inside.txt",
		"sub/keep.txt", "sub/mid.txt", "sub/anchored.txt", "sub/a.log", "sub/rootonly.txt",
		"sub/nested/file.txt", "sub/deep/x.tmp", "sub/deep/special.tmp", "sub/deep/x.txt",
		"sub/deep/a.log", "sub/space file.txt", "sub/Cache", "sub/axxb.txt",
	} {
		o.write(name, "content\n")
	}
	o.mkdir("emptydir")
	o.mkdir("tmp")
	o.mkdir("sub/nested")
}

func (o *oracle) matcher(icase bool) *Matcher {
	root, err := os.OpenRoot(o.dir)
	if err != nil {
		o.t.Fatalf("OpenRoot returned error %v", err)
	}
	o.t.Cleanup(func() { _ = root.Close() })
	return NewMatcher(IgnoreOptions{
		Work:         RootLoader(root),
		Global:       OSLoader(o.dir),
		InfoExclude:  ".git/info/exclude",
		ExcludesFile: filepath.ToSlash(filepath.Join(o.dir, "gitignore-global")),
		IgnoreCase:   icase,
	})
}

func TestIgnoredMatchesGitCheckIgnore(t *testing.T) {
	for _, icase := range []bool{false, true} {
		t.Run("ignorecase="+strconv.FormatBool(icase), func(t *testing.T) {
			o := newOracle(t)
			ignoreFixture(o)
			o.git("config", "core.ignorecase", strconv.FormatBool(icase))
			o.git("config", "core.excludesFile", filepath.ToSlash(filepath.Join(o.dir, "gitignore-global")))

			paths := o.tree()
			paths = append(paths,
				Path{Name: "missing.log"},
				Path{Name: "sub/missing.tmp"},
				Path{Name: "build/missing"},
				Path{Name: "doc/a/b/c/draft.txt"},
				Path{Name: "sub/deep/missing.tmp"},
				Path{Name: "sub/deep/special.tmp"},
				Path{Name: "Cache/inside.txt"},
				Path{Name: "doc/x/tmp"},
				Path{Name: "sub/deep/nested/deeper.txt"},
			)
			if len(paths) < 60 {
				t.Fatalf("fixture produced %d paths, want at least 60", len(paths))
			}
			names := make([]string, len(paths))
			for i, p := range paths {
				names[i] = p.Name
			}
			want := o.checkIgnore(names)
			matcher := o.matcher(icase)
			for match, err := range matcher.Check(paths) {
				if err != nil {
					t.Fatalf("Check(%q) returned error %v", match.Path, err)
				}
				got := ""
				if match.Rule.Valid() {
					got = match.Rule.Source + ":" + strconv.Itoa(match.Rule.Line) + ":" + match.Rule.String()
				}
				if got != want[match.Path] {
					t.Errorf("rule for %q (dir=%v) is %q, git says %q", match.Path, match.IsDir, got, want[match.Path])
				}
				gitIgnored := want[match.Path] != "" && !strings.Contains(want[match.Path], ":!")
				if match.Ignored != gitIgnored {
					t.Errorf("Ignored(%q) = %v, git says %v", match.Path, match.Ignored, gitIgnored)
				}
			}
		})
	}
}

func attributesFixture(o *oracle) []string {
	o.write(".gitattributes", strings.Join([]string{
		"# attributes",
		"* text=auto",
		"*.bin binary",
		`"sp ace.txt" diff=spaced`,
		"[attr]mymacro -text diff merge=custom",
		"sub/** merge=fromroot",
		"*.md eol=lf",
		"unspec.txt !text",
		"onlydir/ diff=dironly",
		"*.c   filter=indent   ident",
		"deep/**/deep.txt diff=deep",
	}, "\n")+"\n")
	o.write("sub/.gitattributes", "a.bin -merge\nb.bin mymacro\n*.md eol=crlf\n[attr]ignored -diff\n")
	o.write("sub/deep/.gitattributes", "x.txt diff=subdeep\n")
	o.write(".git/info/attributes", "*.md text eol=crlf\ninfo.txt diff=frominfo\n")
	o.write("gitattributes-global", "*.glob diff=fromglobal\nglobal.txt merge=fromglobal\n")

	files := []string{
		"x.txt", "a.bin", "sub/a.bin", "sub/b.bin", "sp ace.txt", "r.md", "sub/r.md",
		"unspec.txt", "file.c", "deep/a/b/deep.txt", "sub/deep/x.txt", "info.txt",
		"a.glob", "global.txt", "sub/nothing.txt", "sub/deep/other.txt", "plain",
	}
	for _, name := range files {
		o.write(name, "content\n")
	}
	o.mkdir("onlydir")
	o.mkdir("sub/onlydir")
	return append(files, "onlydir", "onlydir/", "sub/onlydir/", "sub/onlydir")
}

func (o *oracle) attributes() *Attributes {
	root, err := os.OpenRoot(o.dir)
	if err != nil {
		o.t.Fatalf("OpenRoot returned error %v", err)
	}
	o.t.Cleanup(func() { _ = root.Close() })
	return New(AttributeOptions{
		Work:           RootLoader(root),
		Global:         OSLoader(o.dir),
		InfoFile:       ".git/info/attributes",
		AttributesFile: "gitattributes-global",
	})
}

func (o *oracle) checkAttr(paths []string, names ...string) map[string]map[string]string {
	o.t.Helper()
	args := []string{"check-attr"}
	if len(names) == 0 {
		args = append(args, "-a")
	} else {
		args = append(args, names...)
	}
	args = append(args, "--stdin", "-z")
	out, err := o.command(nulJoin(paths), args...)
	if err != nil {
		o.t.Fatalf("%v", err)
	}
	fields := nulFields(out)
	if len(fields)%3 != 0 {
		o.t.Fatalf("check-attr produced %d fields, want a multiple of three", len(fields))
	}
	result := map[string]map[string]string{}
	for _, p := range paths {
		result[p] = map[string]string{}
	}
	for i := 0; i < len(fields); i += 3 {
		result[fields[i]][fields[i+1]] = fields[i+2]
	}
	return result
}

func flatten(values map[string]Value) map[string]string {
	out := map[string]string{}
	for name, value := range values {
		out[name] = value.String()
	}
	return out
}

func compareAttrs(t *testing.T, path string, got, want map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(got)+len(want))
	for name := range got {
		keys = append(keys, name)
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	for _, name := range keys {
		if got[name] != want[name] {
			t.Errorf("attribute %q of %q is %q, git says %q", name, path, got[name], want[name])
		}
	}
}

func TestAllAttributesMatchGitCheckAttr(t *testing.T) {
	o := newOracle(t)
	paths := attributesFixture(o)
	o.git("config", "core.attributesFile", filepath.ToSlash(filepath.Join(o.dir, "gitattributes-global")))
	want := o.checkAttr(paths)
	attrs := o.attributes()
	for _, path := range paths {
		compareAttrs(t, path, flatten(attrs.Get(path)), want[path])
	}
}

func TestNamedAttributesMatchGitCheckAttr(t *testing.T) {
	o := newOracle(t)
	paths := attributesFixture(o)
	o.git("config", "core.attributesFile", filepath.ToSlash(filepath.Join(o.dir, "gitattributes-global")))
	names := []string{"text", "eol", "diff", "merge", "binary", "filter", "ident"}
	want := o.checkAttr(paths, names...)
	attrs := o.attributes()
	for _, path := range paths {
		compareAttrs(t, path, flatten(attrs.Get(path, names...)), want[path])
	}
}

func TestBinaryAndDriversMatchGitCheckAttr(t *testing.T) {
	o := newOracle(t)
	paths := attributesFixture(o)
	o.git("config", "core.attributesFile", filepath.ToSlash(filepath.Join(o.dir, "gitattributes-global")))
	want := o.checkAttr(paths, "diff", "merge", "filter")
	attrs := o.attributes()
	for _, path := range paths {
		if got, expected := attrs.Binary(path), want[path]["diff"] == "unset"; got != expected {
			t.Errorf("Binary(%q) = %v, git reports diff %q", path, got, want[path]["diff"])
		}
		for name, got := range map[string]string{
			"diff": attrs.Diff(path), "merge": attrs.Merge(path), "filter": attrs.Filter(path),
		} {
			expected := want[path][name]
			if expected == "set" || expected == "unset" || expected == "unspecified" {
				expected = ""
			}
			if got != expected {
				t.Errorf("%s driver of %q is %q, git says %q", name, path, got, expected)
			}
		}
	}
}

type eolCase struct {
	name  string
	attrs string
}

var eolCases = []eolCase{
	{"none", ""},
	{"text", "text"},
	{"notext", "-text"},
	{"auto", "text=auto"},
	{"lf", "eol=lf"},
	{"crlf", "eol=crlf"},
	{"textlf", "text eol=lf"},
	{"textcrlf", "text eol=crlf"},
	{"autolf", "text=auto eol=lf"},
	{"autocrlf", "text=auto eol=crlf"},
}

func eolFixture(o *oracle) []string {
	var attrs strings.Builder
	var names []string
	for _, c := range eolCases {
		if c.attrs != "" {
			fmt.Fprintf(&attrs, "%s-* %s\n", c.name, c.attrs)
		}
		for _, kind := range []string{"lf", "crlf"} {
			name := c.name + "-" + kind + ".txt"
			names = append(names, name)
			if kind == "lf" {
				o.write(name, "alpha\nbeta\ngamma\n")
			} else {
				o.write(name, "alpha\r\nbeta\r\ngamma\r\n")
			}
		}
	}
	o.write(".gitattributes", attrs.String())
	return names
}

func (o *oracle) eolAttributes(autoCRLF, eol string) *Attributes {
	root, err := os.OpenRoot(o.dir)
	if err != nil {
		o.t.Fatalf("OpenRoot returned error %v", err)
	}
	o.t.Cleanup(func() { _ = root.Close() })
	return New(AttributeOptions{
		Work:     RootLoader(root),
		AutoCRLF: autoCRLF,
		EOL:      eol,
	})
}

func (o *oracle) lsFilesEOL() map[string]string {
	o.t.Helper()
	out := o.git("ls-files", "--eol")
	result := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		info, path, ok := strings.Cut(line, "\t")
		if !ok {
			o.t.Fatalf("ls-files --eol produced %q", line)
		}
		marker := strings.Index(info, "attr/")
		if marker < 0 {
			o.t.Fatalf("ls-files --eol produced %q without an attribute column", line)
		}
		result[path] = strings.TrimSpace(info[marker+len("attr/"):])
	}
	return result
}

func TestTextPolicyMatchesGitEndOfLineHandling(t *testing.T) {
	for _, autoCRLF := range []string{AutoCRLFFalse, AutoCRLFTrue, AutoCRLFInput} {
		for _, eol := range []string{EOLNative, EOLLF, EOLCRLF} {
			t.Run("autocrlf="+autoCRLF+",eol="+eol, func(t *testing.T) {
				o := newOracle(t)
				names := eolFixture(o)
				o.git("config", "core.autocrlf", autoCRLF)
				o.git("config", "core.eol", eol)
				o.git("add", "-A")
				o.git("commit", "-q", "-m", "fixture")

				attrs := o.eolAttributes(autoCRLF, eol)
				wantAttr := o.lsFilesEOL()
				for _, name := range names {
					policy := attrs.Text(name)
					if got := policy.Attr.String(); got != wantAttr[name] {
						t.Errorf("attribute of %q is %q, git says %q", name, got, wantAttr[name])
					}
				}

				for _, name := range names {
					if !strings.HasSuffix(name, "-crlf.txt") {
						continue
					}
					blob, err := o.command(nil, "cat-file", "blob", ":"+name)
					if err != nil {
						t.Fatalf("%v", err)
					}
					policy := attrs.Text(name)
					wantLF := policy.Convert.OnCheckin == ConvertLF
					if gotLF := !strings.Contains(blob, "\r\n"); gotLF != wantLF {
						t.Errorf("blob of %q has LF endings %v, policy %v predicts %v",
							name, gotLF, policy.Effective, wantLF)
					}
				}

				for _, name := range names {
					if !strings.HasSuffix(name, "-lf.txt") {
						continue
					}
					if err := os.Remove(filepath.Join(o.dir, name)); err != nil {
						t.Fatalf("Remove(%q) returned error %v", name, err)
					}
				}
				o.git("checkout", "--", ".")
				for _, name := range names {
					if !strings.HasSuffix(name, "-lf.txt") {
						continue
					}
					policy := attrs.Text(name)
					wantCRLF := policy.Convert.OnCheckout == ConvertCRLF
					if gotCRLF := bytes.Contains(o.read(name), []byte("\r\n")); gotCRLF != wantCRLF {
						t.Errorf("worktree copy of %q has CRLF endings %v, policy %v predicts %v",
							name, gotCRLF, policy.Effective, wantCRLF)
					}
				}
			})
		}
	}
}

func TestDefaultExcludesFileFollowsXDGLayout(t *testing.T) {
	o := newOracle(t)
	home := filepath.Join(filepath.Dir(o.dir), "home")
	config := filepath.Join(home, ".config")
	got := DefaultExcludesFile(func(key string) string {
		return map[string]string{"XDG_CONFIG_HOME": config, "HOME": home}[key]
	})
	want := filepath.ToSlash(filepath.Join(config, "git", "ignore"))
	if got != want {
		t.Fatalf("DefaultExcludesFile = %q, want %q", got, want)
	}
	if !slices.Contains(o.env, "XDG_CONFIG_HOME="+config) {
		t.Fatalf("oracle environment does not carry XDG_CONFIG_HOME=%q", config)
	}
}
