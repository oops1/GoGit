package attributes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func TestDiffBinaryFollowsTheDiffAttributeAndDriver(t *testing.T) {
	global := filepath.Join(t.TempDir(), "gitconfig")
	settings := "[diff \"conv\"]\n\ttextconv = cat\n" +
		"[diff \"on\"]\n\tbinary = true\n" +
		"[diff \"off\"]\n\tbinary = false\n" +
		"[diff \"auto\"]\n\tbinary = AUTO\n" +
		"[diff \"broken\"]\n\tbinary = maybe\n"
	if err := os.WriteFile(global, []byte(settings), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	cfg, err := config.Load(config.Options{GlobalFile: global, NoSystem: true})
	if err != nil {
		t.Fatalf("config.Load returned error %v", err)
	}
	rules := "*.nodiff -diff\n*.bin binary\n*.set diff\n*.conv diff=conv\n*.on diff=on\n*.off diff=off\n" +
		"*.auto diff=auto\n*.broken diff=broken\n*.plain diff=plain\n"
	withConfig := New(AttributeOptions{Work: MemoryLoader(map[string]string{".gitattributes": rules}), Config: cfg})
	withoutConfig := New(AttributeOptions{Work: MemoryLoader(map[string]string{".gitattributes": rules})})
	cases := []struct {
		attrs  *Attributes
		path   string
		binary bool
		known  bool
	}{
		{withConfig, "x.nodiff", true, true},
		{withConfig, "x.bin", true, true},
		{withConfig, "x.set", false, false},
		{withConfig, "x.none", false, false},
		{withConfig, "x.conv", true, true},
		{withConfig, "x.on", true, true},
		{withConfig, "x.off", false, true},
		{withConfig, "x.auto", false, false},
		{withConfig, "x.broken", false, false},
		{withConfig, "x.plain", false, false},
		{withoutConfig, "x.nodiff", true, true},
		{withoutConfig, "x.on", false, false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if binary, known := c.attrs.DiffBinary(c.path); binary != c.binary || known != c.known {
				t.Fatalf("DiffBinary(%q) returned %v, %v instead of %v, %v", c.path, binary, known, c.binary, c.known)
			}
		})
	}
}
