package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSplitChallengeRecognizesSchemesAndTokens(t *testing.T) {
	cases := []struct {
		value  string
		scheme string
		token  string
	}{
		{"NTLM", "ntlm", ""},
		{"NTLM TlRMTVNTUAA=", "ntlm", "TlRMTVNTUAA="},
		{"Negotiate  YII=  ", "negotiate", "YII="},
		{`Basic realm="git", charset="UTF-8"`, "basic", `realm="git"`},
		{"Bearer", "bearer", ""},
		{`Digest realm="git", nonce="x"`, "digest", `realm="git", nonce="x"`},
		{"Mutual something", "", ""},
	}
	for _, c := range cases {
		scheme, token := splitChallenge(c.value)
		if scheme != c.scheme || token != c.token {
			t.Errorf("splitChallenge(%q) = (%q, %q), want (%q, %q)", c.value, scheme, token, c.scheme, c.token)
		}
	}
}

func TestParseAuthChallengesKeepsTheTokenOverABareScheme(t *testing.T) {
	h := http.Header{}
	h.Add("WWW-Authenticate", "NTLM")
	h.Add("WWW-Authenticate", "NTLM TlRMTVNTUAA=")
	got := parseAuthChallenges(h, headerWWWAuthenticate)
	if got["ntlm"] != "TlRMTVNTUAA=" {
		t.Fatalf("ntlm token = %q, want the base64 challenge", got["ntlm"])
	}
}

func TestSelectAuthSchemeOrdersByPreference(t *testing.T) {
	all := map[string]string{"basic": "", "ntlm": "", "negotiate": "", "bearer": ""}
	if got := selectAuthScheme(all); got != schemeNegotiate {
		t.Fatalf("selectAuthScheme = %q, want negotiate", got)
	}
	if got := selectAuthScheme(map[string]string{"ntlm": "", "basic": ""}); got != schemeNTLM {
		t.Fatalf("selectAuthScheme = %q, want ntlm", got)
	}
	if got := selectAuthScheme(map[string]string{"digest": ""}); got != "" {
		t.Fatalf("selectAuthScheme = %q, want empty for digest only", got)
	}
}

func TestChallengeTokenDecodesBase64AndIgnoresGarbage(t *testing.T) {
	h := http.Header{}
	h.Set("WWW-Authenticate", "NTLM TlRMTVNTUAA=")
	if got := challengeToken(h, headerWWWAuthenticate, schemeNTLM); string(got) != "NTLMSSP\x00" {
		t.Fatalf("challengeToken = %q, want the decoded signature", got)
	}
	bad := http.Header{}
	bad.Set("WWW-Authenticate", "NTLM ****")
	if got := challengeToken(bad, headerWWWAuthenticate, schemeNTLM); got != nil {
		t.Fatalf("challengeToken on invalid base64 = %v, want nil", got)
	}
	if got := challengeToken(http.Header{}, headerWWWAuthenticate, schemeNTLM); got != nil {
		t.Fatalf("challengeToken with no header = %v, want nil", got)
	}
}

func TestNTLMGeneratorRejectsAMalformedChallenge(t *testing.T) {
	gen := newCredentialGenerator(schemeNTLM, newNTLMCredentials("alice", []byte("pw")), nil)
	if _, _, err := gen.next(nil); err != nil {
		t.Fatalf("first next returned %v", err)
	}
	if _, _, err := gen.next([]byte("not ntlm")); err == nil {
		t.Fatalf("second next accepted a malformed challenge")
	}
	gen.close()
}

func TestSPNEGOGeneratorRejectsATokenWithoutNTLM(t *testing.T) {
	gen := newCredentialGenerator(schemeNegotiate, newNTLMCredentials("alice", []byte("pw")), nil)
	if _, _, err := gen.next(nil); err != nil {
		t.Fatalf("first next returned %v", err)
	}
	if _, _, err := gen.next([]byte{0x30, 0x03, 0x02, 0x01, 0x01}); err == nil {
		t.Fatalf("second next accepted a token without an NTLM mech token")
	}
	gen.close()
}

func TestConnectionAuthTreatsADroppedChallengeAsARejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", "NTLM")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	creds := &fakeCredentialSource{creds: []Credentials{{Username: "alice", Password: []byte("pw")}}}
	cfg := testGitConfig(t, "[http]\n\temptyAuth = false\n")
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Config: cfg, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.Advertise(t.Context()); !errors.Is(err, ErrNTLMAuthFailed) {
		t.Fatalf("Advertise returned %v, want ErrNTLMAuthFailed", err)
	}
	if len(creds.retries) != 2 {
		t.Fatalf("credential source asked %d times, want 2", len(creds.retries))
	}
}

func TestLocalWorkstationReturnsAShortUppercaseName(t *testing.T) {
	if got := localWorkstation(); got != strings.ToUpper(got) || strings.Contains(got, ".") {
		t.Fatalf("localWorkstation = %q, want a short uppercase name", got)
	}
}
