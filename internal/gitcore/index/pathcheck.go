package index

import (
	"errors"
	"fmt"
	"runtime"
	"unicode/utf8"

	"github.com/oops1/gogit/internal/gitcore/object"
)

var ErrUnsafePath = errors.New("index: invalid path")

type PathRules struct {
	ProtectNTFS bool
	ProtectHFS  bool
	Windows     bool
}

func DefaultPathRules() PathRules {
	return PathRules{
		ProtectNTFS: true,
		ProtectHFS:  runtime.GOOS == "darwin",
		Windows:     runtime.GOOS == "windows",
	}
}

func VerifyPath(path string, mode object.Mode, rules PathRules) error {
	if !rules.accepts(path, mode) {
		return fmt.Errorf("%w: %q", ErrUnsafePath, path)
	}
	return nil
}

func (x *Index) AddVerified(entry Entry, rules PathRules) error {
	if err := VerifyPath(entry.Path, entry.Mode, rules); err != nil {
		return err
	}
	x.Add(entry)
	return nil
}

func IsDotGitmodules(name string) bool {
	return PathRules{}.isHFSDotGeneric(name, "gitmodules") || isNTFSDotGeneric(name, "gitmodules", "gi7eba")
}

func (r PathRules) separator(c byte) bool {
	return c == '/' || r.Windows && c == '\\'
}

func (r PathRules) accepts(path string, mode object.Mode) bool {
	if r.Windows && (hasDOSDrivePrefix(path) || r.ProtectNTFS && !ValidWin32Path(path)) {
		return false
	}
	at := 0
	for {
		rest := path[at:]
		if r.aliasesDotGit(rest, mode) {
			return false
		}
		if rest == "" {
			return mode.IsTree()
		}
		if rest[0] == '.' && !r.verifyDotfile(rest[1:], mode) || r.separator(rest[0]) {
			return false
		}
		at++
		for {
			if at == len(path) {
				return true
			}
			c := path[at]
			at++
			if r.separator(c) {
				if c == '\\' && r.ProtectNTFS {
					return false
				}
				break
			}
			if c == '\\' && r.ProtectNTFS && r.aliasesDotGitOnNTFS(path[at:], mode) {
				return false
			}
		}
	}
}

func (r PathRules) aliasesDotGit(component string, mode object.Mode) bool {
	if r.ProtectHFS && (r.isHFSDotGeneric(component, "git") || mode.IsSymlink() && r.isHFSDotGeneric(component, "gitmodules")) {
		return true
	}
	return r.ProtectNTFS && r.aliasesDotGitOnNTFS(component, mode)
}

func (r PathRules) aliasesDotGitOnNTFS(component string, mode object.Mode) bool {
	return isNTFSDotGit(component) || mode.IsSymlink() && isNTFSDotGeneric(component, "gitmodules", "gi7eba")
}

func (r PathRules) verifyDotfile(rest string, mode object.Mode) bool {
	if rest == "" || r.separator(rest[0]) {
		return false
	}
	switch rest[0] {
	case 'g', 'G':
		if len(rest) < 3 || lowerASCII(rest[1]) != 'i' || lowerASCII(rest[2]) != 't' {
			break
		}
		if len(rest) == 3 || r.separator(rest[3]) {
			return false
		}
		tail := rest[3:]
		if mode.IsSymlink() && hasPrefixFold(tail, "modules") && (len(tail) == len("modules") || r.separator(tail[len("modules")])) {
			return false
		}
	case '.':
		if len(rest) == 1 || r.separator(rest[1]) {
			return false
		}
	}
	return true
}

func hasDOSDrivePrefix(path string) bool {
	return len(path) >= 2 && isASCIILetter(path[0]) && path[1] == ':'
}

func isASCIILetter(c byte) bool {
	return lowerASCII(c) >= 'a' && lowerASCII(c) <= 'z'
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for at := range len(prefix) {
		if lowerASCII(s[at]) != prefix[at] {
			return false
		}
	}
	return true
}

func isNTFSDotGit(name string) bool {
	var tail string
	switch {
	case len(name) >= 4 && name[0] == '.' && hasPrefixFold(name[1:], "git"):
		tail = name[4:]
	case hasPrefixFold(name, "git~1"):
		tail = name[5:]
	default:
		return false
	}
	for at := range len(tail) {
		c := tail[at]
		if c == '/' || c == '\\' || c == ':' {
			return true
		}
		if c != '.' && c != ' ' {
			return false
		}
	}
	return true
}

func onlySpacesAndPeriods(tail string) bool {
	for at := range len(tail) {
		c := tail[at]
		if c == ':' {
			return true
		}
		if c != '.' && c != ' ' {
			return false
		}
	}
	return true
}

func isNTFSDotGeneric(name, dotName, shortPrefix string) bool {
	if len(name) > 0 && name[0] == '.' && hasPrefixFold(name[1:], dotName) {
		return onlySpacesAndPeriods(name[1+len(dotName):])
	}
	if hasPrefixFold(name, dotName[:6]) && len(name) >= 8 && name[6] == '~' && name[7] >= '1' && name[7] <= '4' {
		return onlySpacesAndPeriods(name[8:])
	}
	at, sawTilde := 0, false
	for ; at < 8; at++ {
		if at >= len(name) {
			return false
		}
		c := name[at]
		switch {
		case sawTilde:
			if c < '0' || c > '9' {
				return false
			}
		case c == '~':
			at++
			if at >= len(name) || name[at] < '1' || name[at] > '9' {
				return false
			}
			sawTilde = true
		case at >= 6 || c >= utf8.RuneSelf || lowerASCII(c) != shortPrefix[at]:
			return false
		}
	}
	return onlySpacesAndPeriods(name[at:])
}

func nextHFSRune(s string) (rune, string) {
	for s != "" {
		c, size := utf8.DecodeRuneInString(s)
		if c == utf8.RuneError && size <= 1 || c == 0xfffe || c == 0xffff {
			return 0, ""
		}
		s = s[size:]
		if hfsIgnorable(c) {
			continue
		}
		return c, s
	}
	return 0, ""
}

func hfsIgnorable(c rune) bool {
	switch {
	case c >= 0x200c && c <= 0x200f, c >= 0x202a && c <= 0x202e, c >= 0x206a && c <= 0x206f, c == 0xfeff:
		return true
	}
	return false
}

func (r PathRules) isHFSDotGeneric(path, needle string) bool {
	c, path := nextHFSRune(path)
	if c != '.' {
		return false
	}
	for at := range len(needle) {
		c, path = nextHFSRune(path)
		if c >= utf8.RuneSelf || lowerASCII(byte(c)) != needle[at] {
			return false
		}
	}
	c, _ = nextHFSRune(path)
	return c == 0 || c < utf8.RuneSelf && r.separator(byte(c))
}

func ValidWin32Path(path string) bool {
	start := 0
	for at := 0; at <= len(path); at++ {
		if at < len(path) && path[at] != '/' && path[at] != '\\' {
			if !validWin32Char(path[at]) {
				return false
			}
			continue
		}
		if !validWin32Segment(path[start:at]) {
			return false
		}
		start = at + 1
	}
	return true
}

func validWin32Char(c byte) bool {
	switch c {
	case ':', '<', '>', '"', '|', '?', '*':
		return false
	}
	return c >= 0x20
}

func validWin32Segment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return true
	}
	if last := segment[len(segment)-1]; last == ' ' || last == '.' {
		return false
	}
	return !reservedWin32Name(segment)
}

var win32DeviceNames = []string{"aux", "nul", "prn", "con", "conin$", "conout$"}

func reservedWin32Name(segment string) bool {
	for _, device := range win32DeviceNames {
		if hasPrefixFold(segment, device) && endsDeviceName(segment[len(device):]) {
			return true
		}
	}
	for _, device := range []string{"com", "lpt"} {
		if hasPrefixFold(segment, device) && len(segment) > 3 && segment[3] >= '1' && segment[3] <= '9' && endsDeviceName(segment[4:]) {
			return true
		}
	}
	return false
}

func endsDeviceName(tail string) bool {
	for tail != "" && tail[0] == ' ' {
		tail = tail[1:]
	}
	return tail == "" || tail[0] == '.'
}
