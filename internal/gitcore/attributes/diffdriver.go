package attributes

import "strings"

func (a *Attributes) DiffBinary(path string) (binary bool, known bool) {
	value := a.Get(path, "diff")["diff"]
	cfg := a.opts.Config
	switch {
	case value.IsUnset():
		return true, true
	case value.IsSet():
		return false, true
	case value.Kind() != Valued || cfg == nil:
		return false, false
	}
	prefix := "diff." + value.Text() + "."
	if command, _ := cfg.Get(prefix + "textconv"); command != "" {
		return true, true
	}
	raw, set := cfg.Get(prefix + "binary")
	if !set || strings.EqualFold(raw, "auto") {
		return false, false
	}
	forced, err := cfg.GetBool(prefix + "binary")
	return forced, err == nil
}
