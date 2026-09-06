package transport

import (
	"errors"
	"testing"
)

func TestCredentialsWipe(t *testing.T) {
	creds := Credentials{Username: "user", Password: []byte("pw"), Token: []byte("tok")}
	creds.Wipe()
	if creds.Password != nil {
		t.Fatalf("Password = %v after Wipe, want nil", creds.Password)
	}
	if creds.Token != nil {
		t.Fatalf("Token = %v after Wipe, want nil", creds.Token)
	}
}

func TestCredentialsWipeOnZeroValue(t *testing.T) {
	var creds Credentials
	creds.Wipe()
}

func TestDialReturnsUnsupportedSchemeForEveryScheme(t *testing.T) {
	tests := []string{
		"/home/user/repo.git",
		`C:\repo`,
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			session, err := Dial(t.Context(), raw, UploadPack, Options{})
			if session != nil {
				t.Fatalf("Dial returned a non-nil Session for %q", raw)
			}
			if !errors.Is(err, ErrUnsupportedScheme) {
				t.Fatalf("Dial(%q) returned %v, want ErrUnsupportedScheme", raw, err)
			}
		})
	}
}

func TestDialSupportsHTTPHTTPSAndGitSchemes(t *testing.T) {
	tests := []string{
		"https://github.com/user/repo.git",
		"http://example.com/repo.git",
		"git://github.com/user/repo.git",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			session, err := Dial(t.Context(), raw, UploadPack, Options{})
			if err != nil {
				t.Fatalf("Dial(%q) returned error %v", raw, err)
			}
			if session == nil {
				t.Fatalf("Dial(%q) returned a nil Session", raw)
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Close returned error %v", err)
			}
		})
	}
}

func TestDialPropagatesURLParseError(t *testing.T) {
	_, err := Dial(t.Context(), "", UploadPack, Options{})
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("Dial returned %v, want ErrInvalidURL", err)
	}
}

func TestDialSupportsSSHSchemeAndSCPLikeForm(t *testing.T) {
	tests := []string{
		"ssh://user:secret@github.com/repo.git",
		"git@github.com:user/repo.git",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			session, err := Dial(t.Context(), raw, UploadPack, Options{})
			if err != nil {
				t.Fatalf("Dial(%q) returned error %v", raw, err)
			}
			ssh, ok := session.(*sshSession)
			if !ok {
				t.Fatalf("Dial(%q) returned a %T, want *sshSession", raw, session)
			}
			if err := ssh.Close(); err != nil {
				t.Fatalf("Close returned error %v", err)
			}
		})
	}
}

func TestDialKeepsThePasswordForHTTPSessions(t *testing.T) {
	session, err := Dial(t.Context(), "https://user:secret@github.com/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	http, ok := session.(*httpSession)
	if !ok {
		t.Fatalf("Dial returned a %T, want *httpSession", session)
	}
	if string(http.password) != "secret" {
		t.Fatalf("password = %q, want %q", http.password, "secret")
	}
	if err := http.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if http.password != nil {
		t.Fatalf("password = %v after Close, want nil", http.password)
	}
}
