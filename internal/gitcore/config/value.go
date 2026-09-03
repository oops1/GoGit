package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type lookuper interface {
	value(n name) (string, bool, bool)
	allValues(n name) []string
}

func getString(src lookuper, key string) (string, bool) {
	n, err := parseName(key)
	if err != nil {
		return "", false
	}
	v, _, ok := src.value(n)
	return v, ok
}

func getAll(src lookuper, key string) []string {
	n, err := parseName(key)
	if err != nil {
		return nil
	}
	return src.allValues(n)
}

func has(src lookuper, key string) bool {
	n, err := parseName(key)
	if err != nil {
		return false
	}
	_, _, ok := src.value(n)
	return ok
}

func getBool(src lookuper, key string) (bool, error) {
	raw, set, err := rawValue(src, key)
	if err != nil {
		return false, err
	}
	if !set {
		return true, nil
	}
	b, err := ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func getInt(src lookuper, key string) (int64, error) {
	raw, set, err := rawValue(src, key)
	if err != nil {
		return 0, err
	}
	if !set {
		return 0, fmt.Errorf("%w: %s", ErrMissingValue, key)
	}
	v, err := ParseInt(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return v, nil
}

func getPath(src lookuper, key string) (string, error) {
	raw, set, err := rawValue(src, key)
	if err != nil {
		return "", err
	}
	if !set {
		return "", fmt.Errorf("%w: %s", ErrMissingValue, key)
	}
	p, err := ExpandPath(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	return p, nil
}

func rawValue(src lookuper, key string) (string, bool, error) {
	n, err := parseName(key)
	if err != nil {
		return "", false, err
	}
	v, set, ok := src.value(n)
	if !ok {
		return "", false, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return v, set, nil
}

func ParseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "yes", "on":
		return true, nil
	case "", "false", "no", "off":
		return false, nil
	}
	n, err := ParseInt(s)
	if err != nil {
		return false, fmt.Errorf("%w: %q", ErrInvalidBool, s)
	}
	return n != 0, nil
}

func ParseInt(s string) (int64, error) {
	num := s
	factor := int64(1)
	if n := len(num); n > 0 {
		switch num[n-1] {
		case 'k', 'K':
			factor = 1 << 10
		case 'm', 'M':
			factor = 1 << 20
		case 'g', 'G':
			factor = 1 << 30
		}
		if factor != 1 {
			num = num[:n-1]
		}
	}
	num = strings.TrimLeft(num, " \t\n\v\f\r")
	if strings.ContainsRune(num, '_') {
		return 0, fmt.Errorf("%w: %q", ErrInvalidInt, s)
	}
	v, err := strconv.ParseInt(num, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidInt, s)
	}
	result := v * factor
	if result/factor != v {
		return 0, fmt.Errorf("%w: %q", ErrInvalidInt, s)
	}
	return result, nil
}

func ExpandPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	rest := p[1:]
	if rest != "" && rest[0] != '/' && rest[0] != '\\' {
		return "", fmt.Errorf("%w: %q", ErrExpandUser, p)
	}
	home, ok := homeDir()
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrExpandUser, p)
	}
	return filepath.Join(home, rest), nil
}

func homeDir() (string, bool) {
	if h := os.Getenv("HOME"); h != "" {
		return h, true
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h, true
	}
	return "", false
}
