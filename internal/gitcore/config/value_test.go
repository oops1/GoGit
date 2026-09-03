package config

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestParseBoolFollowsGit(t *testing.T) {
	tests := []struct {
		in   string
		want bool
		ok   bool
	}{
		{"true", true, true},
		{"TRUE", true, true},
		{"yes", true, true},
		{"On", true, true},
		{"1", true, true},
		{"42", true, true},
		{"-1", true, true},
		{"false", false, true},
		{"NO", false, true},
		{"off", false, true},
		{"0", false, true},
		{"", false, true},
		{"maybe", false, false},
		{"1x", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseBool(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("ParseBool(%q) error = %v, want ok=%v", tc.in, err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidBool) {
				t.Fatalf("ParseBool(%q) error = %v, want ErrInvalidBool", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseBool(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseIntUnderstandsUnits(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"12", 12, true},
		{"-12", -12, true},
		{"+12", 12, true},
		{"0x10", 16, true},
		{"010", 8, true},
		{"2k", 2048, true},
		{"2K", 2048, true},
		{"3m", 3 << 20, true},
		{"3M", 3 << 20, true},
		{"4g", 4 << 30, true},
		{"4G", 4 << 30, true},
		{"-1k", -1024, true},
		{"", 0, false},
		{"k", 0, false},
		{"1kk", 0, false},
		{"1_000", 0, false},
		{"abc", 0, false},
		{strconv.FormatInt(math.MaxInt64, 10) + "k", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseInt(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("ParseInt(%q) = %v, error %v, want ok=%v", tc.in, got, err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidInt) {
				t.Fatalf("ParseInt(%q) error = %v, want ErrInvalidInt", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseInt(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandPathHandlesTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plainPathIsUnchanged", "/etc/x", "/etc/x", true},
		{"emptyPathIsUnchanged", "", "", true},
		{"tildeAlone", "~", home, true},
		{"tildeSlash", "~/a/b", filepath.Join(home, "a", "b"), true},
		{"tildeBackslash", `~\a`, backslashExpanded(home), runtime.GOOS == "windows"},
		{"otherUserIsNotExpanded", "~someone/a", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpandPath(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("ExpandPath(%q) error = %v, want ok=%v", tc.in, err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, ErrExpandUser) {
				t.Fatalf("ExpandPath(%q) error = %v, want ErrExpandUser", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ExpandPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandPathFailsWithoutHome(t *testing.T) {
	isolateEnv(t)
	if _, err := ExpandPath("~/x"); !errors.Is(err, ErrExpandUser) {
		t.Fatalf("error = %v, want ErrExpandUser", err)
	}
}

func TestHomeDirPrefersHomeVariable(t *testing.T) {
	isolateEnv(t)
	if _, ok := homeDir(); ok {
		t.Fatal("homeDir found a home directory with every variable cleared")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if got, ok := homeDir(); !ok || got != dir {
		t.Fatalf("homeDir = %q, %v, want %q", got, ok, dir)
	}
	t.Setenv("HOME", "")
	t.Setenv(userHomeEnvKey(), dir)
	if got, ok := homeDir(); !ok || got != dir {
		t.Fatalf("homeDir from the platform variable = %q, %v, want %q", got, ok, dir)
	}
}

func userHomeEnvKey() string {
	if os.PathSeparator == '\\' {
		return "USERPROFILE"
	}
	return "HOME"
}

func TestTypedGettersReportMissingAndInvalidValues(t *testing.T) {
	f := mustParse(t, "[a]\n\tflag\n\tbad = maybe\n\tnum = x\n\tpath = ~who/x\n")
	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"boolOfMissingKey", func() error { _, err := f.GetBool("a.zz"); return err }, ErrNotFound},
		{"boolOfBadValue", func() error { _, err := f.GetBool("a.bad"); return err }, ErrInvalidBool},
		{"boolOfBadName", func() error { _, err := f.GetBool("zz"); return err }, ErrInvalidName},
		{"intOfMissingKey", func() error { _, err := f.GetInt("a.zz"); return err }, ErrNotFound},
		{"intOfValuelessKey", func() error { _, err := f.GetInt("a.flag"); return err }, ErrMissingValue},
		{"intOfBadValue", func() error { _, err := f.GetInt("a.num"); return err }, ErrInvalidInt},
		{"pathOfMissingKey", func() error { _, err := f.GetPath("a.zz"); return err }, ErrNotFound},
		{"pathOfValuelessKey", func() error { _, err := f.GetPath("a.flag"); return err }, ErrMissingValue},
		{"pathOfOtherUser", func() error { _, err := f.GetPath("a.path"); return err }, ErrExpandUser},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
	if got := f.GetAll("zz"); got != nil {
		t.Errorf("GetAll of an invalid name = %q", got)
	}
	if f.Has("zz") {
		t.Error("Has accepted an invalid name")
	}
}

func backslashExpanded(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "a")
	}
	return ""
}
