package config

import "testing"

func TestSetAllReplacesEveryValueAtOnce(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		key    string
		values []string
		want   string
	}{
		{
			name:   "one value for several",
			text:   "[a]\n\tb = 1\n\tc = x\n\tb = 2\n",
			key:    "a.b",
			values: []string{"9"},
			want:   "[a]\n\tb = 9\n\tc = x\n",
		},
		{
			name:   "several values for one",
			text:   "[a]\n\tb = 1\n",
			key:    "a.b",
			values: []string{"7", "8"},
			want:   "[a]\n\tb = 7\n\tb = 8\n",
		},
		{
			name: "no values at all leaves the rest alone",
			text: "[a]\n\tb = 1\n\tc = x\n",
			key:  "a.b",
			want: "[a]\n\tc = x\n",
		},
		{
			name:   "a key that was not there yet",
			text:   "[a]\n\tc = x\n",
			key:    "a.b",
			values: []string{"1"},
			want:   "[a]\n\tc = x\n\tb = 1\n",
		},
		{
			name:   "a section that was not there yet",
			text:   "[a]\n\tc = x\n",
			key:    "d.b",
			values: []string{"1"},
			want:   "[a]\n\tc = x\n[d]\n\tb = 1\n",
		},
		{
			name:   "a subsection key",
			text:   "[remote \"origin\"]\n\turl = https://example.com/a.git\n\tpushurl = https://example.com/a.git\n",
			key:    "remote.origin.pushurl",
			values: []string{"https://example.com/a.git", "http://mirror.example/a.git"},
			want:   "[remote \"origin\"]\n\turl = https://example.com/a.git\n\tpushurl = https://example.com/a.git\n\tpushurl = http://mirror.example/a.git\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := mustParse(t, tc.text)
			if err := f.SetAll(tc.key, tc.values); err != nil {
				t.Fatalf("SetAll returned error %v", err)
			}
			if got := string(f.Encode()); got != tc.want {
				t.Fatalf("Encode = %q, want %q", got, tc.want)
			}
			if got := f.GetAll(tc.key); len(got) != len(tc.values) {
				t.Fatalf("GetAll = %v, want %v", got, tc.values)
			}
		})
	}
}
