package config

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

type Level int

const (
	LevelSystem Level = iota
	LevelGlobal
	LevelLocal
	LevelWorktree
	LevelCommand
)

func (l Level) String() string {
	switch l {
	case LevelSystem:
		return "system"
	case LevelGlobal:
		return "global"
	case LevelLocal:
		return "local"
	case LevelWorktree:
		return "worktree"
	case LevelCommand:
		return "command"
	}
	return "unknown"
}

type Origin struct {
	Level Level
	Path  string
	Line  int
}

type Entry struct {
	Section       string
	Subsection    string
	HasSubsection bool
	Key           string
	Value         string
	HasValue      bool
	Origin        Origin
}

func (e Entry) Name() string {
	return joinName(e.Section, e.Subsection, e.HasSubsection, e.Key)
}

type Config struct {
	entries []Entry
	files   map[Level]*File
}

type Options struct {
	GitDir      string
	WorktreeDir string
	Branch      string
	SystemFile  string
	GlobalFile  string
	NoSystem    bool
}

const maxIncludeDepth = 10

const commandLineOrigin = "command line"

type loader struct {
	opts     Options
	cfg      *Config
	branch   string
	branchOK bool
}

func Load(opts Options) (*Config, error) {
	l := &loader{opts: opts, cfg: &Config{files: map[Level]*File{}}}
	steps := []func() error{l.system, l.global, l.local, l.worktree, l.command}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return l.cfg, nil
}

func (l *loader) system() error {
	if l.opts.NoSystem {
		return nil
	}
	skip, err := envBool("GIT_CONFIG_NOSYSTEM")
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	path := l.opts.SystemFile
	if path == "" {
		if v, ok := os.LookupEnv("GIT_CONFIG_SYSTEM"); ok {
			path = v
		} else {
			path = defaultSystemPath()
		}
	}
	if path == "" {
		return nil
	}
	return l.addFile(LevelSystem, path)
}

func (l *loader) global() error {
	if l.opts.GlobalFile != "" {
		return l.addFile(LevelGlobal, l.opts.GlobalFile)
	}
	if v, ok := os.LookupEnv("GIT_CONFIG_GLOBAL"); ok {
		if v == "" {
			return nil
		}
		return l.addFile(LevelGlobal, v)
	}
	for _, path := range globalPaths() {
		if err := l.addFile(LevelGlobal, path); err != nil {
			return err
		}
	}
	return nil
}

func globalPaths() []string {
	var paths []string
	home, hasHome := homeDir()
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "git", "config"))
	} else if hasHome {
		paths = append(paths, filepath.Join(home, ".config", "git", "config"))
	}
	if hasHome {
		paths = append(paths, filepath.Join(home, ".gitconfig"))
	}
	return paths
}

func defaultSystemPath() string {
	return systemPathFor(runtime.GOOS, os.Getenv("ProgramData"))
}

func systemPathFor(goos, programData string) string {
	if goos == "windows" {
		if programData == "" {
			return ""
		}
		return filepath.Join(programData, "Git", "config")
	}
	return "/etc/gitconfig"
}

func (l *loader) local() error {
	if l.opts.GitDir == "" {
		return nil
	}
	return l.addFile(LevelLocal, filepath.Join(l.opts.GitDir, "config"))
}

func (l *loader) worktree() error {
	if l.opts.GitDir == "" {
		return nil
	}
	on, err := l.cfg.boolOr("extensions.worktreeConfig", false)
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	dir := l.opts.WorktreeDir
	if dir == "" {
		dir = l.opts.GitDir
	}
	return l.addFile(LevelWorktree, filepath.Join(dir, "config.worktree"))
}

func (l *loader) command() error {
	raw := os.Getenv("GIT_CONFIG_COUNT")
	if raw == "" {
		return nil
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return fmt.Errorf("%w: %q", ErrInvalidEnvCount, raw)
	}
	for i := range count {
		key, keyOK := os.LookupEnv(fmt.Sprintf("GIT_CONFIG_KEY_%d", i))
		value, valueOK := os.LookupEnv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", i))
		if !keyOK || !valueOK {
			return fmt.Errorf("%w: %d", ErrMissingEnvEntry, i)
		}
		n, err := parseName(key)
		if err != nil {
			return err
		}
		l.cfg.entries = append(l.cfg.entries, Entry{
			Section:       n.section,
			Subsection:    n.sub,
			HasSubsection: n.hasSub,
			Key:           n.key,
			Value:         value,
			HasValue:      true,
			Origin:        Origin{Level: LevelCommand, Path: commandLineOrigin},
		})
	}
	return nil
}

func envBool(key string) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return false, nil
	}
	b, err := ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func readFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.path = path
	return f, nil
}

func (l *loader) addFile(level Level, path string) error {
	f, err := readFile(path)
	if err != nil || f == nil {
		return err
	}
	l.cfg.files[level] = f
	return l.expand(level, f, 0, []string{filepath.Clean(path)})
}

func (l *loader) expand(level Level, f *File, depth int, stack []string) error {
	for v := range f.Variables() {
		l.cfg.entries = append(l.cfg.entries, Entry{
			Section:       v.Section,
			Subsection:    v.Subsection,
			HasSubsection: v.HasSubsection,
			Key:           v.Key,
			Value:         v.Value,
			HasValue:      v.HasValue,
			Origin:        Origin{Level: level, Path: f.path, Line: v.Line},
		})
		if !l.wantsInclude(v, f.path) {
			continue
		}
		target, err := resolveInclude(v.Value, f.path)
		if err != nil {
			return err
		}
		if slices.Contains(stack, target) {
			return fmt.Errorf("%w: %s", ErrIncludeCycle, target)
		}
		if depth+1 > maxIncludeDepth {
			return fmt.Errorf("%w: %s", ErrIncludeDepth, target)
		}
		included, err := readFile(target)
		if err != nil {
			return err
		}
		if included == nil {
			continue
		}
		if err := l.expand(level, included, depth+1, append(stack, target)); err != nil {
			return err
		}
	}
	return nil
}

func (l *loader) wantsInclude(v Variable, base string) bool {
	if v.Key != "path" || !v.HasValue {
		return false
	}
	if v.Section == "include" && !v.HasSubsection {
		return true
	}
	if v.Section == "includeif" && v.HasSubsection {
		return l.condition(v.Subsection, base)
	}
	return false
}

func (l *loader) condition(cond, base string) bool {
	if pattern, ok := strings.CutPrefix(cond, "gitdir:"); ok {
		return l.matchGitDir(pattern, base, false)
	}
	if pattern, ok := strings.CutPrefix(cond, "gitdir/i:"); ok {
		return l.matchGitDir(pattern, base, true)
	}
	if pattern, ok := strings.CutPrefix(cond, "onbranch:"); ok {
		return l.matchBranch(pattern)
	}
	return false
}

func (l *loader) matchGitDir(pattern, base string, icase bool) bool {
	if l.opts.GitDir == "" {
		return false
	}
	expanded, ok := expandGitDirPattern(pattern, base)
	if !ok {
		return false
	}
	return wildMatch(expanded, filepath.ToSlash(realPath(l.opts.GitDir)), icase)
}

func expandGitDirPattern(pattern, base string) (string, bool) {
	if pattern == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(pattern, "./"):
		pattern = filepath.ToSlash(filepath.Dir(realPath(base))) + "/" + pattern[2:]
	case strings.HasPrefix(pattern, "~/"):
		home, ok := homeDir()
		if !ok {
			return "", false
		}
		pattern = filepath.ToSlash(realPath(home)) + "/" + pattern[2:]
	case !isAbsolutePattern(pattern):
		pattern = "**/" + pattern
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	return pattern, true
}

func isAbsolutePattern(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return len(p) >= 3 && isAlpha(p[0]) && p[1] == ':' && (p[2] == '/' || p[2] == '\\')
}

func realPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

func (l *loader) matchBranch(pattern string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	branch := l.currentBranch()
	if branch == "" {
		return false
	}
	return wildMatch(pattern, branch, false)
}

func (l *loader) currentBranch() string {
	if !l.branchOK {
		l.branch = l.readBranch()
		l.branchOK = true
	}
	return l.branch
}

func (l *loader) readBranch() string {
	if l.opts.Branch != "" {
		return l.opts.Branch
	}
	if l.opts.GitDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(l.opts.GitDir, "HEAD"))
	if err != nil {
		return ""
	}
	if ref, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: refs/heads/"); ok {
		return ref
	}
	return ""
}

func resolveInclude(path, base string) (string, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(filepath.Dir(base), expanded)
	}
	return filepath.Clean(expanded), nil
}

func (c *Config) All() iter.Seq[Entry] {
	return slices.Values(c.entries)
}

func (c *Config) Len() int { return len(c.entries) }

func (c *Config) File(level Level) (*File, bool) {
	f, ok := c.files[level]
	return f, ok
}

func (c *Config) value(n name) (string, bool, bool) {
	var found *Entry
	for i := range c.entries {
		if c.entries[i].matches(n) {
			found = &c.entries[i]
		}
	}
	if found == nil {
		return "", false, false
	}
	return found.Value, found.HasValue, true
}

func (c *Config) allValues(n name) []string {
	var out []string
	for i := range c.entries {
		if c.entries[i].matches(n) {
			out = append(out, c.entries[i].Value)
		}
	}
	return out
}

func (e *Entry) matches(n name) bool {
	return e.Key == n.key && e.Section == n.section && e.HasSubsection == n.hasSub && e.Subsection == n.sub
}

func (c *Config) Get(key string) (string, bool)      { return getString(c, key) }
func (c *Config) GetAll(key string) []string         { return getAll(c, key) }
func (c *Config) Has(key string) bool                { return has(c, key) }
func (c *Config) GetBool(key string) (bool, error)   { return getBool(c, key) }
func (c *Config) GetInt(key string) (int64, error)   { return getInt(c, key) }
func (c *Config) GetPath(key string) (string, error) { return getPath(c, key) }

func (c *Config) Lookup(key string) (Entry, bool) {
	n, err := parseName(key)
	if err != nil {
		return Entry{}, false
	}
	var found *Entry
	for i := range c.entries {
		if c.entries[i].matches(n) {
			found = &c.entries[i]
		}
	}
	if found == nil {
		return Entry{}, false
	}
	return *found, true
}

func (c *Config) Origin(key string) (Origin, bool) {
	e, ok := c.Lookup(key)
	return e.Origin, ok
}

func (c *Config) Subsections(section string) []string {
	section = strings.ToLower(section)
	var out []string
	for i := range c.entries {
		e := &c.entries[i]
		if e.Section == section && e.HasSubsection && !slices.Contains(out, e.Subsection) {
			out = append(out, e.Subsection)
		}
	}
	slices.Sort(out)
	return out
}

func (c *Config) boolOr(key string, def bool) (bool, error) {
	v, err := c.GetBool(key)
	if errors.Is(err, ErrNotFound) {
		return def, nil
	}
	return v, err
}

func (c *Config) stringOr(key, def string) string {
	if v, ok := c.Get(key); ok {
		return v
	}
	return def
}

func (c *Config) intOr(key string, def int64) (int64, error) {
	v, err := c.GetInt(key)
	if errors.Is(err, ErrNotFound) {
		return def, nil
	}
	return v, err
}

func (c *Config) pathOr(key, def string) (string, error) {
	v, err := c.GetPath(key)
	if errors.Is(err, ErrNotFound) {
		return def, nil
	}
	return v, err
}
