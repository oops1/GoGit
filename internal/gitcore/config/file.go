package config

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type File struct {
	path  string
	items []*item
}

type Variable struct {
	Section       string
	Subsection    string
	HasSubsection bool
	Key           string
	Value         string
	HasValue      bool
	Line          int
}

func (v Variable) Name() string {
	return joinName(v.Section, v.Subsection, v.HasSubsection, v.Key)
}

func joinName(section, subsection string, hasSub bool, key string) string {
	if hasSub {
		return section + "." + subsection + "." + key
	}
	return section + "." + key
}

type name struct {
	section string
	sub     string
	hasSub  bool
	key     string
}

func parseName(s string) (name, error) {
	first := strings.IndexByte(s, '.')
	last := strings.LastIndexByte(s, '.')
	if first <= 0 || last == len(s)-1 {
		return name{}, fmt.Errorf("%w: %q", ErrInvalidName, s)
	}
	n := name{section: strings.ToLower(s[:first]), key: strings.ToLower(s[last+1:])}
	if first != last {
		n.sub = s[first+1 : last]
		n.hasSub = true
	}
	if !validSectionName(n.section) || !validKeyName(n.key) || strings.ContainsRune(n.sub, '\n') {
		return name{}, fmt.Errorf("%w: %q", ErrInvalidName, s)
	}
	return n, nil
}

func parseSectionName(s string) (name, error) {
	n := name{}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		n.section = strings.ToLower(s[:i])
		n.sub = s[i+1:]
		n.hasSub = true
	} else {
		n.section = strings.ToLower(s)
	}
	if !validSectionName(n.section) || strings.ContainsRune(n.sub, '\n') {
		return name{}, fmt.Errorf("%w: %q", ErrInvalidSection, s)
	}
	return n, nil
}

func validSectionName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isKeyChar(s[i]) {
			return false
		}
	}
	return true
}

func validKeyName(s string) bool {
	if s == "" || !isAlpha(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isKeyChar(s[i]) {
			return false
		}
	}
	return true
}

func (f *File) Path() string { return f.path }

func (f *File) Encode() []byte {
	size := 0
	for _, it := range f.items {
		size += len(it.text())
	}
	out := make([]byte, 0, size)
	for _, it := range f.items {
		out = append(out, it.text()...)
	}
	return out
}

func (f *File) Variables() iter.Seq[Variable] {
	return func(yield func(Variable) bool) {
		line := 1
		var sec *item
		for _, it := range f.items {
			switch it.kind {
			case kindSection:
				sec = it
			case kindEntry:
				if sec != nil {
					v := Variable{
						Section:       sec.name,
						Subsection:    sec.sub,
						HasSubsection: sec.hasSub,
						Key:           it.name,
						Value:         it.value,
						HasValue:      it.hasValue,
						Line:          line,
					}
					if !yield(v) {
						return
					}
				}
			}
			line += strings.Count(it.text(), "\n")
		}
	}
}

func (f *File) find(n name) []int {
	var out []int
	var sec *item
	for i, it := range f.items {
		switch it.kind {
		case kindSection:
			sec = it
		case kindEntry:
			if sec != nil && it.name == n.key && sec.matchesSection(n) {
				out = append(out, i)
			}
		}
	}
	return out
}

func (f *File) findSections(n name) []int {
	var out []int
	for i, it := range f.items {
		if it.kind == kindSection && it.matchesSection(n) {
			out = append(out, i)
		}
	}
	return out
}

func (f *File) value(n name) (string, bool, bool) {
	idx := f.find(n)
	if len(idx) == 0 {
		return "", false, false
	}
	it := f.items[idx[len(idx)-1]]
	return it.value, it.hasValue, true
}

func (f *File) allValues(n name) []string {
	idx := f.find(n)
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		out = append(out, f.items[i].value)
	}
	return out
}

func (f *File) Get(key string) (string, bool)      { return getString(f, key) }
func (f *File) GetAll(key string) []string         { return getAll(f, key) }
func (f *File) Has(key string) bool                { return has(f, key) }
func (f *File) GetBool(key string) (bool, error)   { return getBool(f, key) }
func (f *File) GetInt(key string) (int64, error)   { return getInt(f, key) }
func (f *File) GetPath(key string) (string, error) { return getPath(f, key) }

func (f *File) Set(key, value string) error {
	n, err := parseName(key)
	if err != nil {
		return err
	}
	idx := f.find(n)
	if len(idx) == 0 {
		f.insert(n, value)
		return nil
	}
	f.items[idx[0]].setValue(value)
	for i := len(idx) - 1; i > 0; i-- {
		f.deleteEntry(idx[i])
	}
	return nil
}

func (f *File) Add(key, value string) error {
	n, err := parseName(key)
	if err != nil {
		return err
	}
	f.insert(n, value)
	return nil
}

func (f *File) Unset(key string) error {
	n, err := parseName(key)
	if err != nil {
		return err
	}
	idx := f.find(n)
	if len(idx) == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	f.deleteEntry(idx[len(idx)-1])
	return nil
}

func (f *File) UnsetAll(key string) error {
	n, err := parseName(key)
	if err != nil {
		return err
	}
	idx := f.find(n)
	if len(idx) == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	for i := len(idx) - 1; i >= 0; i-- {
		f.deleteEntry(idx[i])
	}
	return nil
}

func (f *File) RemoveSection(section string) error {
	n, err := parseSectionName(section)
	if err != nil {
		return err
	}
	idx := f.findSections(n)
	if len(idx) == 0 {
		return fmt.Errorf("%w: %s", ErrSectionNotFound, section)
	}
	for i := len(idx) - 1; i >= 0; i-- {
		start, atLineStart := f.lineStart(idx[i])
		if !atLineStart {
			start = idx[i]
		}
		end := idx[i] + 1
		for end < len(f.items) && f.items[end].kind != kindSection {
			end++
		}
		f.items = slices.Delete(f.items, start, end)
	}
	return nil
}

func (f *File) RenameSection(oldName, newName string) error {
	from, err := parseSectionName(oldName)
	if err != nil {
		return err
	}
	to, err := parseSectionName(newName)
	if err != nil {
		return err
	}
	idx := f.findSections(from)
	if len(idx) == 0 {
		return fmt.Errorf("%w: %s", ErrSectionNotFound, oldName)
	}
	for _, i := range idx {
		it := f.items[i]
		it.raw = sectionHeader(to)
		it.name, it.sub, it.hasSub = to.section, to.sub, to.hasSub
	}
	return nil
}

func (f *File) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, f.Encode())
}

func writeAtomic(path string, data []byte) error {
	lock := path + ".lock"
	fh, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = fh.Write(data)
	err = errors.Join(err, fh.Close())
	if err == nil {
		err = os.Rename(lock, path)
	}
	if err != nil {
		err = errors.Join(err, os.Remove(lock))
	}
	return err
}

func (it *item) setValue(value string) {
	switch {
	case it.assign == "":
		it.assign = " = "
	case it.rawValue == "" && it.assign[len(it.assign)-1] == '=':
		it.assign += " "
	}
	it.rawValue = encodeValue(value)
	it.value = value
	it.hasValue = true
}

func newEntry(key, value string) *item {
	return &item{
		kind:     kindEntry,
		rawName:  key,
		name:     key,
		assign:   " = ",
		rawValue: encodeValue(value),
		value:    value,
		hasValue: true,
	}
}

func newSection(n name) *item {
	return &item{kind: kindSection, raw: sectionHeader(n), name: n.section, sub: n.sub, hasSub: n.hasSub}
}

func sectionHeader(n name) string {
	if !n.hasSub {
		return "[" + n.section + "]"
	}
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(n.section)
	b.WriteString(` "`)
	for i := 0; i < len(n.sub); i++ {
		if c := n.sub[i]; c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(n.sub[i])
	}
	b.WriteString(`"]`)
	return b.String()
}

func encodeValue(value string) string {
	var b strings.Builder
	quote := needQuote(value)
	if quote {
		b.WriteByte('"')
	}
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	if quote {
		b.WriteByte('"')
	}
	return b.String()
}

func needQuote(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == ' ' || value[len(value)-1] == ' ' {
		return true
	}
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case ';', '#', '\r', '\v', '\f':
			return true
		}
	}
	return false
}

func (f *File) lineStart(i int) (int, bool) {
	j := i
	for j > 0 && f.items[j-1].kind == kindSpace {
		j--
	}
	return j, j == 0 || f.items[j-1].kind == kindNewline
}

func (f *File) lineEnd(i int) int {
	j := i + 1
	for j < len(f.items) && (f.items[j].kind == kindSpace || f.items[j].kind == kindComment) {
		j++
	}
	if j < len(f.items) && f.items[j].kind == kindNewline {
		j++
	}
	return j
}

func (f *File) deleteEntry(i int) {
	start, atLineStart := f.lineStart(i)
	end := f.lineEnd(i)
	if !atLineStart {
		start = i
		if f.items[end-1].kind == kindNewline {
			end--
		}
	}
	f.items = slices.Delete(f.items, start, end)
}

func (f *File) insert(n name, value string) {
	at, ok := f.insertPoint(n)
	if !ok {
		f.appendSection(n, value)
		return
	}
	add := []*item{{kind: kindSpace, raw: "\t"}, newEntry(n.key, value), {kind: kindNewline, raw: "\n"}}
	if at > 0 && f.items[at-1].kind != kindNewline {
		add = append([]*item{{kind: kindNewline, raw: "\n"}}, add...)
	}
	f.items = slices.Insert(f.items, at, add...)
}

func (f *File) insertPoint(n name) (int, bool) {
	if idx := f.find(n); len(idx) > 0 {
		return f.lineEnd(idx[len(idx)-1]), true
	}
	secs := f.findSections(n)
	if len(secs) == 0 {
		return 0, false
	}
	head := secs[len(secs)-1]
	last := head
	for j := head + 1; j < len(f.items) && f.items[j].kind != kindSection; j++ {
		if f.items[j].kind == kindEntry {
			last = j
		}
	}
	return f.lineEnd(last), true
}

func (f *File) appendSection(n name, value string) {
	if len(f.items) > 0 && f.items[len(f.items)-1].kind != kindNewline {
		f.items = append(f.items, &item{kind: kindNewline, raw: "\n"})
	}
	f.items = append(f.items,
		newSection(n),
		&item{kind: kindNewline, raw: "\n"},
		&item{kind: kindSpace, raw: "\t"},
		newEntry(n.key, value),
		&item{kind: kindNewline, raw: "\n"},
	)
}
