package transport

import "testing"

func TestSafeURLHidesOnlyThePassword(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"password", "https://valeriy:secret@github.com/oops1/gogit.git", "https://valeriy:***@github.com/oops1/gogit.git"},
		{"password and port", "http://user:pw@192.168.3.5:8081/home/winline.git", "http://user:***@192.168.3.5:8081/home/winline.git"},
		{"user without password", "https://valeriy@github.com/oops1/gogit.git", "https://valeriy@github.com/oops1/gogit.git"},
		{"no userinfo", "https://github.com/oops1/gogit.git", "https://github.com/oops1/gogit.git"},
		{"at sign in the path only", "https://github.com/oops1/go@git.git", "https://github.com/oops1/go@git.git"},
		{"scp like", "git@github.com:oops1/gogit.git", "git@github.com:oops1/gogit.git"},
		{"local path", "D:\\Projects\\GoLang\\Go.Git", "D:\\Projects\\GoLang\\Go.Git"},
		{"empty", "", ""},
		{"password with a colon inside", "https://user:pa:ss@host/repo.git", "https://user:***@host/repo.git"},
		{"query after the authority", "https://user:pw@host?a=1", "https://user:***@host?a=1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SafeURL(c.raw); got != c.want {
				t.Fatalf("SafeURL(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}
