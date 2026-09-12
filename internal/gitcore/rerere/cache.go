package rerere

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	CacheDir      = "rr-cache"
	MergeRRFile   = "MERGE_RR"
	preimageName  = "preimage"
	postimageName = "postimage"
	thisimageName = "thisimage"
	dirMode       = 0o777
	fileMode      = 0o666
)

var ErrCorruptMergeRR = errors.New("rerere: MERGE_RR is corrupt")

var ErrNotAConflictID = errors.New("rerere: not a conflict id")

type Cache struct {
	dir string
}

func CacheExists(gitDir string) bool {
	info, err := os.Stat(filepath.Join(gitDir, CacheDir))
	return err == nil && info.IsDir()
}

func OpenCache(gitDir string) (*Cache, error) {
	dir := filepath.Join(gitDir, CacheDir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, err
	}
	return &Cache{dir: dir}, nil
}

func (c *Cache) Dir() string { return c.dir }

type Variant struct {
	Index        int
	HasPreimage  bool
	HasPostimage bool
}

func (v Variant) Complete() bool { return v.HasPreimage && v.HasPostimage }

func validID(id string) bool {
	if len(id) == 0 {
		return false
	}
	for i := range len(id) {
		switch c := id[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func (c *Cache) Variants(id string) []Variant {
	dir, err := c.path(id, "")
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byIndex := map[int]Variant{}
	for _, entry := range entries {
		name, index, ok := splitVariant(entry.Name())
		if !ok {
			continue
		}
		seen := byIndex[index]
		seen.Index = index
		switch name {
		case preimageName:
			seen.HasPreimage = true
		case postimageName:
			seen.HasPostimage = true
		default:
			continue
		}
		byIndex[index] = seen
	}
	variants := make([]Variant, 0, len(byIndex))
	for _, variant := range byIndex {
		variants = append(variants, variant)
	}
	slices.SortFunc(variants, func(a, b Variant) int { return a.Index - b.Index })
	return variants
}

func splitVariant(file string) (string, int, bool) {
	name, suffix, found := strings.Cut(file, ".")
	if !found {
		return name, 0, true
	}
	index, err := strconv.Atoi(suffix)
	if err != nil || index <= 0 {
		return "", 0, false
	}
	return name, index, true
}

func variantFile(name string, index int) string {
	if index <= 0 {
		return name
	}
	return name + "." + strconv.Itoa(index)
}

func (c *Cache) Preimage(id string, index int) ([]byte, error) {
	return c.read(id, variantFile(preimageName, index))
}

func (c *Cache) Postimage(id string, index int) ([]byte, error) {
	return c.read(id, variantFile(postimageName, index))
}

func (c *Cache) WritePreimage(id string, index int, data []byte) error {
	return c.write(id, variantFile(preimageName, index), data)
}

func (c *Cache) WritePostimage(id string, index int, data []byte) error {
	return c.write(id, variantFile(postimageName, index), data)
}

func (c *Cache) WriteThisimage(id string, index int, data []byte) error {
	return c.write(id, variantFile(thisimageName, index), data)
}

func (c *Cache) RemoveVariant(id string, index int) error {
	var errs []error
	for _, name := range []string{preimageName, postimageName, thisimageName} {
		full, err := c.path(id, variantFile(name, index))
		if err != nil {
			return err
		}
		if removed := os.Remove(full); removed != nil && !errors.Is(removed, fs.ErrNotExist) {
			errs = append(errs, removed)
		}
	}
	return errors.Join(errs...)
}

func (c *Cache) VariantForAPreimage(id string) int {
	variants := c.Variants(id)
	for index := 0; ; index++ {
		taken := slices.IndexFunc(variants, func(v Variant) bool { return v.Index == index })
		if taken < 0 || !variants[taken].HasPostimage {
			return index
		}
	}
}

func (c *Cache) path(id, file string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("%w: %q", ErrNotAConflictID, id)
	}
	return filepath.Join(c.dir, id, file), nil
}

func (c *Cache) read(id, file string) ([]byte, error) {
	full, err := c.path(id, file)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (c *Cache) write(id, file string, data []byte) error {
	dir, err := c.path(id, "")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, file), data, fileMode)
}

type Entry struct {
	ID      string
	Variant int
	Path    string
}

func ParseMergeRR(data []byte) ([]Entry, error) {
	var entries []Entry
	for record := range strings.SplitSeq(strings.TrimSuffix(string(data), "\x00"), "\x00") {
		if record == "" {
			continue
		}
		name, path, found := strings.Cut(record, "\t")
		if !found || path == "" {
			return nil, ErrCorruptMergeRR
		}
		id, index, ok := splitVariant(name)
		if !ok || !validID(id) {
			return nil, ErrCorruptMergeRR
		}
		entries = append(entries, Entry{ID: id, Variant: index, Path: path})
	}
	return entries, nil
}

func FormatMergeRR(entries []Entry) []byte {
	var out strings.Builder
	for _, entry := range entries {
		out.WriteString(variantFile(entry.ID, entry.Variant))
		out.WriteString("\t")
		out.WriteString(entry.Path)
		out.WriteByte(0)
	}
	return []byte(out.String())
}
