package console

import (
	"errors"
	"strings"
)

var (
	ErrUnknownOption      = errors.New("console: unknown option")
	ErrOptionNeedsValue   = errors.New("console: the option needs a value")
	ErrOptionTakesNoValue = errors.New("console: the option takes no value")
)

type optionKind uint8

const (
	optionPlain optionKind = iota
	optionValued
)

type option struct {
	long  string
	short string
	kind  optionKind
}

func plain(long, short string) option {
	return option{long: long, short: short, kind: optionPlain}
}

func valued(long, short string) option {
	return option{long: long, short: short, kind: optionValued}
}

type options struct {
	seen map[string]string
	rest []string
	mark int
}

func (o options) has(long string) bool {
	_, ok := o.seen[long]
	return ok
}

func (o options) value(long string) string { return o.seen[long] }

func (o options) args() []string { return o.rest }

func (o options) split() (before, after []string) {
	if o.mark < 0 {
		return o.rest, nil
	}
	return o.rest[:o.mark], o.rest[o.mark:]
}

func (o options) anyOf(longs ...string) bool {
	for _, long := range longs {
		if o.has(long) {
			return true
		}
	}
	return false
}

func findLong(specs []option, name string) (option, bool) {
	for _, spec := range specs {
		if spec.long == name {
			return spec, true
		}
	}
	return option{}, false
}

func findShort(specs []option, name string) (option, bool) {
	for _, spec := range specs {
		if spec.short != "" && spec.short == name {
			return spec, true
		}
	}
	return option{}, false
}

func parseOptions(args []string, specs []option) (options, error) {
	out := options{seen: map[string]string{}, mark: -1}
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch {
		case arg == "--":
			out.mark = len(out.rest)
			out.rest = append(out.rest, args...)
			return out, nil
		case strings.HasPrefix(arg, "--"):
			var err error
			if args, err = out.takeLong(arg, args, specs); err != nil {
				return options{}, err
			}
		case len(arg) > 1 && arg[0] == '-':
			var err error
			if args, err = out.takeShort(arg, args, specs); err != nil {
				return options{}, err
			}
		default:
			out.rest = append(out.rest, arg)
		}
	}
	return out, nil
}

func (o *options) takeLong(arg string, args []string, specs []option) ([]string, error) {
	name, inline, hasInline := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	spec, ok := findLong(specs, name)
	if !ok {
		return nil, detail(ErrUnknownOption, arg)
	}
	if spec.kind == optionPlain {
		if hasInline {
			return nil, detail(ErrOptionTakesNoValue, arg)
		}
		o.seen[spec.long] = ""
		return args, nil
	}
	if hasInline {
		o.seen[spec.long] = inline
		return args, nil
	}
	if len(args) == 0 {
		return nil, detail(ErrOptionNeedsValue, arg)
	}
	o.seen[spec.long] = args[0]
	return args[1:], nil
}

func (o *options) takeShort(arg string, args []string, specs []option) ([]string, error) {
	letters := []rune(strings.TrimPrefix(arg, "-"))
	for i, letter := range letters {
		name := string(letter)
		spec, ok := findShort(specs, name)
		if !ok {
			return nil, detail(ErrUnknownOption, "-"+name)
		}
		if spec.kind == optionPlain {
			o.seen[spec.long] = ""
			continue
		}
		if tail := string(letters[i+1:]); tail != "" {
			o.seen[spec.long] = tail
			return args, nil
		}
		if len(args) == 0 {
			return nil, detail(ErrOptionNeedsValue, "-"+name)
		}
		o.seen[spec.long] = args[0]
		return args[1:], nil
	}
	return args, nil
}
