package credential

import (
	"bytes"
	"context"
	"errors"
	"strings"
)

const (
	wincredFilter        = "git:*"
	savedByHelperComment = "saved by git-credential-wincred"
)

type winCredential struct {
	target   string
	userName string
	comment  string
	blob     []byte
}

type credentialManager interface {
	enumerate(filter string) ([]winCredential, error)
	read(target string) (winCredential, bool, error)
	write(cred winCredential) error
	remove(target string) error
}

func wipeWinCredentials(creds []winCredential) {
	for i := range creds {
		clear(creds[i].blob)
	}
}

type wincredHelper struct {
	name    string
	manager credentialManager
}

func (h *wincredHelper) Name() string { return h.name }

func (h *wincredHelper) Get(_ context.Context, q Query) (Answer, bool, error) {
	if !wincredQueryUsable(q) {
		return Answer{}, false, nil
	}
	creds, err := h.manager.enumerate(wincredFilter)
	if err != nil {
		return Answer{}, false, err
	}
	defer wipeWinCredentials(creds)
	for _, c := range creds {
		if wincredMatches(c, q, nil) {
			return Answer{Username: c.userName, Password: wincredPassword(c.blob)}, true, nil
		}
	}
	return Answer{}, false, nil
}

func (h *wincredHelper) Store(_ context.Context, q Query, a Answer) error {
	q = q.withAnswer(a)
	if !wincredQueryUsable(q) || q.Username == "" || len(a.Password) == 0 {
		return nil
	}
	blob := utf16LEFromUTF8(a.Password)
	defer clear(blob)
	return h.manager.write(winCredential{
		target:   wincredTarget(q),
		userName: q.Username,
		comment:  savedByHelperComment,
		blob:     blob,
	})
}

func (h *wincredHelper) Erase(_ context.Context, q Query, a Answer) error {
	q = q.withAnswer(a)
	if !wincredQueryUsable(q) {
		return nil
	}
	creds, err := h.manager.enumerate(wincredFilter)
	if err != nil {
		return err
	}
	defer wipeWinCredentials(creds)
	var errs error
	for _, c := range creds {
		if wincredMatches(c, q, a.Password) {
			errs = errors.Join(errs, h.manager.remove(c.target))
		}
	}
	return errs
}

func wincredQueryUsable(q Query) bool {
	return q.Protocol != "" && (q.Host != "" || q.Path != "")
}

func wincredTarget(q Query) string {
	var b strings.Builder
	b.WriteString("git:")
	b.WriteString(q.Protocol)
	b.WriteString("://")
	if q.Username != "" {
		b.WriteString(q.Username)
		b.WriteByte('@')
	}
	b.WriteString(q.Host)
	if q.Path != "" {
		b.WriteByte('/')
		b.WriteString(q.Path)
	}
	return b.String()
}

func wincredMatches(c winCredential, q Query, password []byte) bool {
	if q.Username != "" && c.userName != q.Username {
		return false
	}
	target := c.target
	if !wincredMatchPart(&target, "git", ":", false) ||
		!wincredMatchPart(&target, q.Protocol, "://", false) ||
		!wincredMatchPart(&target, q.Username, "@", true) ||
		!wincredMatchPart(&target, q.Host, "/", false) ||
		!wincredMatchPart(&target, q.Path, "", false) {
		return false
	}
	if len(password) == 0 {
		return true
	}
	stored := utf8FromUTF16LE(c.blob)
	defer clear(stored)
	return secretsEqual(stored, password)
}

func wincredMatchPart(target *string, want, delim string, last bool) bool {
	start := *target
	pos := len(start)
	switch {
	case delim == "":
	case last:
		pos = strings.LastIndex(start, delim)
	default:
		pos = strings.Index(start, delim)
	}
	length := pos
	if pos < 0 {
		length = len(start)
	}
	switch {
	case pos >= 0:
		*target = start[pos+len(delim):]
	case want != "":
		*target = ""
	}
	return want == "" || start[:length] == want
}

func wincredPassword(blob []byte) []byte {
	text := utf8FromUTF16LE(blob)
	defer clear(text)
	isLineBreak := func(r rune) bool { return r == '\r' || r == '\n' }
	start := bytes.IndexFunc(text, func(r rune) bool { return !isLineBreak(r) })
	if start < 0 {
		return []byte{}
	}
	line := text[start:]
	if end := bytes.IndexFunc(line, isLineBreak); end >= 0 {
		line = line[:end]
	}
	return bytes.Clone(line)
}
