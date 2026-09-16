package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type testHook struct {
	log      string
	stdin    bool
	appendTo string
	empty    bool
	print    string
	exit     int
}

type hookRecord struct {
	name  string
	args  []string
	stdin []string
}

func recordsEqual(a, b []hookRecord) bool {
	return slices.EqualFunc(a, b, func(x, y hookRecord) bool {
		return x.name == y.name && slices.Equal(x.args, y.args) && slices.Equal(x.stdin, y.stdin)
	})
}

func hookNames(records []hookRecord) []string {
	names := make([]string, 0, len(records))
	for _, r := range records {
		names = append(names, r.name)
	}
	return names
}

func readHookLog(t testing.TB, path string) []hookRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	var records []hookRecord
	for line := range strings.SplitSeq(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "== "); ok {
			fields := strings.Fields(rest)
			records = append(records, hookRecord{name: fields[0], args: fields[1:]})
			continue
		}
		if line != "" && len(records) > 0 {
			last := &records[len(records)-1]
			last.stdin = append(last.stdin, line)
		}
	}
	return records
}

func hookLogPath(t testing.TB) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "hooks.log")
}

func writeHookFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
}
