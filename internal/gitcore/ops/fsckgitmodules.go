package ops

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
)

const (
	FsckGitmodulesName    = "gitmodulesName"
	FsckGitmodulesURL     = "gitmodulesUrl"
	FsckGitmodulesPath    = "gitmodulesPath"
	FsckGitmodulesUpdate  = "gitmodulesUpdate"
	FsckGitmodulesSymlink = "gitmodulesSymlink"
	FsckGitmodulesBlob    = "gitmodulesBlob"
	FsckGitmodulesMissing = "gitmodulesMissing"
)

var ErrBadGitmodules = errors.New("ops: .gitmodules breaks a rule git enforces")

var curlSchemes = []string{"http", "https", "ftp", "ftps"}

type gitmodulesCheck struct {
	blobs    map[hash.ObjectID]struct{}
	problems []FsckProblem
}

func newGitmodulesCheck() *gitmodulesCheck {
	return &gitmodulesCheck{blobs: map[hash.ObjectID]struct{}{}}
}

func (c *gitmodulesCheck) report(check string, id hash.ObjectID, detail string) {
	c.problems = append(c.problems, FsckProblem{Kind: FsckBadGitmodules, ID: id, Check: check, Err: fmt.Errorf("%w: %s", ErrBadGitmodules, detail)})
}

func (c *gitmodulesCheck) inspectTree(id hash.ObjectID, entries []object.TreeEntry) {
	for _, entry := range entries {
		switch {
		case !index.IsDotGitmodules(entry.Name):
		case entry.Mode.IsSymlink():
			c.report(FsckGitmodulesSymlink, id, ".gitmodules is a symbolic link")
		default:
			c.blobs[entry.ID] = struct{}{}
		}
	}
}

func (c *gitmodulesCheck) inspectUnreachable(db *odb.DB, ids []hash.ObjectID) {
	for _, id := range ids {
		kind, data, err := dbGet(db, id)
		if err != nil || kind != object.TypeTree {
			continue
		}
		if tree, err := object.ParseTree(data); err == nil {
			c.inspectTree(id, tree.Entries)
		}
	}
}

func (c *gitmodulesCheck) finish(db *odb.DB) []FsckProblem {
	for _, id := range slices.SortedFunc(maps.Keys(c.blobs), hash.ObjectID.Compare) {
		kind, data, err := dbGet(db, id)
		switch {
		case err != nil:
			c.report(FsckGitmodulesMissing, id, "unable to read .gitmodules blob")
		case kind != object.TypeBlob:
			c.report(FsckGitmodulesBlob, id, "non-blob found at .gitmodules")
		default:
			c.inspectBlob(id, data)
		}
	}
	return c.problems
}

func (c *gitmodulesCheck) inspectBlob(id hash.ObjectID, data []byte) {
	variables, _ := config.ParseVariables(data)
	for _, v := range variables {
		if v.Section != "submodule" || !v.HasSubsection {
			continue
		}
		if !submoduleNameAllowed(v.Subsection) {
			c.report(FsckGitmodulesName, id, "disallowed submodule name: "+v.Subsection)
		}
		switch {
		case !v.HasValue:
		case v.Key == "url" && !submoduleURLAllowed(v.Value):
			c.report(FsckGitmodulesURL, id, "disallowed submodule url: "+v.Value)
		case v.Key == "path" && strings.HasPrefix(v.Value, "-"):
			c.report(FsckGitmodulesPath, id, "disallowed submodule path: "+v.Value)
		case v.Key == "update" && strings.HasPrefix(v.Value, "!"):
			c.report(FsckGitmodulesUpdate, id, "disallowed submodule update setting: "+v.Value)
		}
	}
}

func submoduleNameAllowed(name string) bool {
	separator := func(c rune) bool { return c == '/' || c == '\\' }
	return name != "" && !slices.Contains(strings.FieldsFunc(name, separator), "..")
}

func startsWithDotSeparator(s string) bool {
	return len(s) >= 2 && s[0] == '.' && os.IsPathSeparator(s[1])
}

func startsWithDotDotSeparator(s string) bool {
	return len(s) >= 3 && s[0] == '.' && startsWithDotSeparator(s[1:])
}

func skipLeadingDotdots(url string) (string, int) {
	count := 0
	for {
		switch {
		case startsWithDotDotSeparator(url):
			url, count = url[3:], count+1
		case startsWithDotSeparator(url):
			url = url[2:]
		default:
			return url, count
		}
	}
}

func submoduleURLAllowed(url string) bool {
	if strings.HasPrefix(url, "-") {
		return false
	}
	if startsWithDotSeparator(url) || startsWithDotDotSeparator(url) || strings.HasPrefix(url, "git://") {
		rest, dotdots := skipLeadingDotdots(url)
		escapes := dotdots > 0 && (strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "/"))
		return !escapes && !strings.ContainsRune(urlDecode(url), '\n')
	}
	if curl, ok := curlURL(url); ok {
		return curlURLAllowed(curl)
	}
	return true
}

func urlDecode(s string) string {
	var out strings.Builder
	for at := 0; at < len(s); at++ {
		if s[at] == '%' && at+3 <= len(s) {
			if value, err := strconv.ParseUint(s[at+1:at+3], 16, 8); err == nil && value != 0 {
				out.WriteByte(byte(value))
				at += 2
				continue
			}
		}
		out.WriteByte(s[at])
	}
	return out.String()
}

func curlURL(url string) (string, bool) {
	for _, scheme := range curlSchemes {
		if rest, ok := strings.CutPrefix(url, scheme+"::"); ok {
			return rest, true
		}
		if strings.HasPrefix(url, scheme+"://") {
			return url, true
		}
	}
	return "", false
}

func curlURLAllowed(url string) bool {
	protocol, rest, found := strings.Cut(url, "://")
	if !found || protocol == "" {
		return false
	}
	hostEnd := strings.IndexAny(rest, "/?#")
	if hostEnd < 0 {
		hostEnd = len(rest)
	}
	host, credentials := rest[:hostEnd], ""
	if at := strings.IndexByte(rest, '@'); at >= 0 && at < hostEnd {
		host, credentials = rest[at+1:hostEnd], rest[:at]
	}
	decodedHost := urlDecode(host)
	return decodedHost != "" && !strings.ContainsRune(protocol+urlDecode(credentials)+decodedHost+urlDecode(rest[hostEnd:]), '\n')
}
