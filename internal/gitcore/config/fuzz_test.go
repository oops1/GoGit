package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"[a]\n\tb = c\n",
		"\xef\xbb\xbf[a]b=c",
		"[a \"s\\\"b\"]\n\tk = \"v\"\n",
		"[a.B]\n\tx = 1\n",
		"[a]\n\tb = one\\\ntwo # c\n",
		"[a]\n\tb = \"  x  \" ; c\n",
		"[a]\n\tb\n\tb =\n\tb = 1\n",
		"[a]\r\n\tb = c\r\n",
		"[a] b = c [d] e = f\n",
		"[a]\n\tb = x\\t\\n\\b\\\\\\\"y\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	for _, name := range []string{"local.config", "global.config", "tricky.config", "crlf.config"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatalf("ReadFile(%q) returned error %v", name, err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Parse(data)
		if err != nil {
			return
		}
		encoded := file.Encode()
		if string(encoded) != string(data) {
			t.Fatalf("Encode is not byte identical:\n got %q\nwant %q", encoded, data)
		}
		again, err := Parse(encoded)
		if err != nil {
			t.Fatalf("re-parsing an encoded file failed: %v", err)
		}
		if !slices.Equal(dump(file), dump(again)) {
			t.Fatalf("round trip changed variables:\n got %q\nwant %q", dump(again), dump(file))
		}
	})
}
