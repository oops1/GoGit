package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/assets"
)

const Fallback = "en"

type Table map[string]string

type Catalog map[string]Table

func LoadFS(fsys fs.FS) (Catalog, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	cat := Catalog{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var table Table
		if err := json.Unmarshal(data, &table); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", name, err)
		}
		cat[Code(name)] = table
	}
	return cat, nil
}

func Code(fileName string) string {
	return strings.ToLower(strings.TrimSuffix(path.Base(fileName), ".json"))
}

func (c Catalog) Merge(other Catalog) {
	for code, table := range other {
		dst, ok := c[code]
		if !ok {
			dst = Table{}
			c[code] = dst
		}
		for k, v := range table {
			dst[k] = v
		}
	}
}

func (c Catalog) Codes() []string {
	codes := make([]string, 0, len(c))
	for code := range c {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func (c Catalog) MissingKeys(reference string) map[string][]string {
	ref, ok := c[reference]
	if !ok {
		return nil
	}
	out := map[string][]string{}
	for code, table := range c {
		if code == reference {
			continue
		}
		for key := range ref {
			if _, ok := table[key]; !ok {
				out[code] = append(out[code], key)
			}
		}
		sort.Strings(out[code])
	}
	return out
}

func (c Catalog) Register() {
	for code, table := range c {
		widget.RegisterStrings(code, table)
	}
	widget.SetFallbackLanguage(Fallback)
}

func Builtin() (Catalog, error) {
	return LoadFS(assets.I18N())
}

func Install(userDir string) (Catalog, error) {
	return InstallFrom(assets.I18N(), userDir)
}

func InstallFrom(builtin fs.FS, userDir string) (Catalog, error) {
	cat, err := LoadFS(builtin)
	if err != nil {
		return nil, err
	}
	if userDir != "" {
		user, err := LoadFS(os.DirFS(userDir))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		cat.Merge(user)
	}
	cat.Register()
	return cat, nil
}

func Apply(code string) {
	widget.SetLanguage(code)
}

func Current() string {
	return strings.ToLower(widget.Language())
}

func T(key string) string {
	return widget.Tr(key)
}

func Tf(key string, args ...any) string {
	return widget.Trf(key, args...)
}
