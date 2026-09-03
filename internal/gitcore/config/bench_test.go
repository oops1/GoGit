package config

import (
	"fmt"
	"strings"
	"testing"
)

func benchmarkSource() []byte {
	var b strings.Builder
	b.WriteString("# generated benchmark configuration\n")
	for section := range 20 {
		fmt.Fprintf(&b, "[section%d \"sub %d\"]\n", section, section)
		for key := range 7 {
			switch key % 3 {
			case 0:
				fmt.Fprintf(&b, "\tkey%d = value-%d-%d\n", key, section, key)
			case 1:
				fmt.Fprintf(&b, "\tkey%d = \"quoted \\t value %d\" # comment\n", key, key)
			default:
				fmt.Fprintf(&b, "\tkey%d = first\\\n\tsecond\n", key)
			}
		}
	}
	return []byte(b.String())
}

func BenchmarkParse(b *testing.B) {
	data := benchmarkSource()
	if lines := strings.Count(string(data), "\n"); lines != 201 {
		b.Fatalf("benchmark source has %d lines, want 201", lines)
	}
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := Parse(data); err != nil {
			b.Fatalf("Parse returned error %v", err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	f, err := Parse(benchmarkSource())
	if err != nil {
		b.Fatalf("Parse returned error %v", err)
	}
	for b.Loop() {
		f.Encode()
	}
}
