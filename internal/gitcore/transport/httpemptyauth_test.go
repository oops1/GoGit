package transport

import "testing"

func TestResolveHTTPSettingsReadsEmptyAuth(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   bool
	}{
		{"unset", "[http]\n\tproxy = p:1\n", false},
		{"true", "[http]\n\temptyAuth = true\n", true},
		{"bare key is true", "[http]\n\temptyAuth\n", true},
		{"false", "[http]\n\temptyAuth = false\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := resolveHTTPSettings(testGitConfig(t, tc.config), "origin", settingsEndpoint)
			if err != nil {
				t.Fatalf("resolveHTTPSettings returned error %v", err)
			}
			if s.emptyAuth != tc.want {
				t.Fatalf("emptyAuth = %v, want %v", s.emptyAuth, tc.want)
			}
		})
	}
}

func TestResolveHTTPSettingsRejectsBadEmptyAuth(t *testing.T) {
	if _, err := resolveHTTPSettings(testGitConfig(t, "[http]\n\temptyAuth = maybe\n"), "origin", settingsEndpoint); err == nil {
		t.Fatalf("resolveHTTPSettings accepted a non-boolean emptyAuth")
	}
}
