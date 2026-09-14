package config

import (
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type pathStep struct {
	name    string
	element string
}

type extraValue struct {
	path  []pathStep
	value any
}

func (e extraValue) id() string {
	var b strings.Builder
	for _, step := range e.path {
		b.WriteString(step.name)
		b.WriteString("[")
		b.WriteString(step.element)
		b.WriteString("].")
	}
	return b.String()
}

func collectExtras(raw map[string]any, keys []toml.Key) []extraValue {
	var out []extraValue
	seen := map[string]bool{}
	for _, key := range keys {
		for _, extra := range collectAt(raw, key, nil) {
			if id := extra.id(); !seen[id] {
				seen[id] = true
				out = append(out, extra)
			}
		}
	}
	return out
}

func collectAt(node any, key []string, prefix []pathStep) []extraValue {
	if len(key) == 0 {
		return []extraValue{{path: prefix, value: node}}
	}
	switch n := node.(type) {
	case map[string]any:
		child, ok := n[key[0]]
		if !ok {
			return nil
		}
		return collectAt(child, key[1:], append(slices.Clone(prefix), pathStep{name: key[0]}))
	case []map[string]any:
		var out []extraValue
		for i, element := range n {
			scoped := slices.Clone(prefix)
			scoped[len(scoped)-1].element = elementID(element, i)
			out = append(out, collectAt(element, key, scoped)...)
		}
		return out
	default:
		return nil
	}
}

func elementID(element map[string]any, index int) string {
	if id, ok := element["id"].(string); ok {
		return "id=" + id
	}
	return "#" + strconv.Itoa(index)
}

func insertExtra(node map[string]any, path []pathStep, value any) {
	last := len(path) - 1
	for i, step := range path {
		child, exists := node[step.name]
		switch {
		case i == last:
			if !exists {
				node[step.name] = value
			}
			return
		case step.element != "":
			node = findElement(child, step.element)
		default:
			table, ok := child.(map[string]any)
			if !ok && !exists {
				table = map[string]any{}
				node[step.name] = table
			}
			node = table
		}
		if node == nil {
			return
		}
	}
}

func findElement(child any, id string) map[string]any {
	elements, _ := child.([]map[string]any)
	for i, element := range elements {
		if elementID(element, i) == id {
			return element
		}
	}
	return nil
}

func mergeExtras(ours, theirs []extraValue) []extraValue {
	out := slices.Clone(theirs)
	for _, extra := range ours {
		id := extra.id()
		if !slices.ContainsFunc(theirs, func(t extraValue) bool { return t.id() == id }) {
			out = append(out, extra)
		}
	}
	return out
}
