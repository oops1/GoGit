package config

import (
	"fmt"
	"strings"
)

type Remote struct {
	Name     string
	URLs     []string
	PushURLs []string
	Fetch    []string
	Push     []string
}

type Branch struct {
	Name   string
	Remote string
	Merge  []string
	Rebase string
}

type User struct {
	Name       string
	Email      string
	SigningKey string
}

type Core struct {
	Bare                    bool
	RepositoryFormatVersion int64
	Worktree                string
	AutoCRLF                string
	EOL                     string
	FileMode                bool
	Symlinks                bool
	IgnoreCase              bool
	ExcludesFile            string
	HooksPath               string
}

type Extensions struct {
	ObjectFormat    string
	WorktreeConfig  bool
	PreciousObjects bool
}

const (
	AutoCRLFInput = "input"
	AutoCRLFTrue  = "true"
	AutoCRLFFalse = "false"
)

const (
	EOLNative = "native"
	EOLLF     = "lf"
	EOLCRLF   = "crlf"
)

const (
	ObjectFormatSHA1   = "sha1"
	ObjectFormatSHA256 = "sha256"
)

func (c *Config) Remotes() []Remote {
	names := c.Subsections("remote")
	out := make([]Remote, 0, len(names))
	for _, n := range names {
		out = append(out, c.remote(n))
	}
	return out
}

func (c *Config) Remote(name string) (Remote, bool) {
	if !c.hasSubsection("remote", name) {
		return Remote{}, false
	}
	return c.remote(name), true
}

func (c *Config) remote(name string) Remote {
	key := func(k string) string { return joinName("remote", name, true, k) }
	return Remote{
		Name:     name,
		URLs:     c.GetAll(key("url")),
		PushURLs: c.GetAll(key("pushurl")),
		Fetch:    c.GetAll(key("fetch")),
		Push:     c.GetAll(key("push")),
	}
}

func (c *Config) Branches() []Branch {
	names := c.Subsections("branch")
	out := make([]Branch, 0, len(names))
	for _, n := range names {
		out = append(out, c.branch(n))
	}
	return out
}

func (c *Config) Branch(name string) (Branch, bool) {
	if !c.hasSubsection("branch", name) {
		return Branch{}, false
	}
	return c.branch(name), true
}

func (c *Config) branch(name string) Branch {
	key := func(k string) string { return joinName("branch", name, true, k) }
	return Branch{
		Name:   name,
		Remote: c.stringOr(key("remote"), ""),
		Merge:  c.GetAll(key("merge")),
		Rebase: c.stringOr(key("rebase"), ""),
	}
}

func (c *Config) hasSubsection(section, sub string) bool {
	for i := range c.entries {
		e := &c.entries[i]
		if e.Section == section && e.HasSubsection && e.Subsection == sub {
			return true
		}
	}
	return false
}

func (c *Config) User() User {
	return User{
		Name:       c.stringOr("user.name", ""),
		Email:      c.stringOr("user.email", ""),
		SigningKey: c.stringOr("user.signingkey", ""),
	}
}

func (c *Config) Core() (Core, error) {
	core := Core{
		AutoCRLF: AutoCRLFFalse,
		EOL:      EOLNative,
		FileMode: true,
		Symlinks: true,
	}
	var err error
	if core.Bare, err = c.boolOr("core.bare", false); err != nil {
		return Core{}, err
	}
	if core.RepositoryFormatVersion, err = c.intOr("core.repositoryformatversion", 0); err != nil {
		return Core{}, err
	}
	if core.FileMode, err = c.boolOr("core.filemode", true); err != nil {
		return Core{}, err
	}
	if core.Symlinks, err = c.boolOr("core.symlinks", true); err != nil {
		return Core{}, err
	}
	if core.IgnoreCase, err = c.boolOr("core.ignorecase", false); err != nil {
		return Core{}, err
	}
	if core.Worktree, err = c.pathOr("core.worktree", ""); err != nil {
		return Core{}, err
	}
	if core.ExcludesFile, err = c.pathOr("core.excludesfile", ""); err != nil {
		return Core{}, err
	}
	if core.HooksPath, err = c.pathOr("core.hookspath", ""); err != nil {
		return Core{}, err
	}
	if core.AutoCRLF, err = parseAutoCRLF(c.stringOr("core.autocrlf", AutoCRLFFalse)); err != nil {
		return Core{}, err
	}
	if core.EOL, err = parseEOL(c.stringOr("core.eol", EOLNative)); err != nil {
		return Core{}, err
	}
	return core, nil
}

func entryBool(e *Entry) (bool, error) {
	if !e.HasValue {
		return true, nil
	}
	return ParseBool(e.Value)
}

func parseAutoCRLF(raw string) (string, error) {
	if strings.EqualFold(raw, AutoCRLFInput) {
		return AutoCRLFInput, nil
	}
	on, err := ParseBool(raw)
	if err != nil {
		return "", fmt.Errorf("core.autocrlf: %w", err)
	}
	if on {
		return AutoCRLFTrue, nil
	}
	return AutoCRLFFalse, nil
}

func parseEOL(raw string) (string, error) {
	switch strings.ToLower(raw) {
	case EOLNative, EOLLF, EOLCRLF:
		return strings.ToLower(raw), nil
	}
	return "", fmt.Errorf("core.eol: %w: %q", ErrInvalidValue, raw)
}

func (c *Config) Extensions() (Extensions, error) {
	ext := Extensions{ObjectFormat: ObjectFormatSHA1}
	for i := range c.entries {
		e := &c.entries[i]
		if e.Section != "extensions" || e.HasSubsection {
			continue
		}
		switch e.Key {
		case "objectformat":
			if !e.HasValue {
				return Extensions{}, fmt.Errorf("%w: extensions.objectformat", ErrMissingValue)
			}
			switch strings.ToLower(e.Value) {
			case ObjectFormatSHA1, ObjectFormatSHA256:
				ext.ObjectFormat = strings.ToLower(e.Value)
			default:
				return Extensions{}, fmt.Errorf("extensions.objectformat: %w: %q", ErrInvalidValue, e.Value)
			}
		case "worktreeconfig", "preciousobjects":
			on, err := entryBool(e)
			if err != nil {
				return Extensions{}, fmt.Errorf("extensions.%s: %w", e.Key, err)
			}
			if e.Key == "worktreeconfig" {
				ext.WorktreeConfig = on
			} else {
				ext.PreciousObjects = on
			}
		default:
			return Extensions{}, fmt.Errorf("%w: extensions.%s", ErrUnknownExtension, e.Key)
		}
	}
	return ext, nil
}
