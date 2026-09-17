package transport

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type ntlmTestServer struct {
	t               *testing.T
	password        string
	scheme          string
	offer           []string
	serverChallenge []byte
	targetInfo      []byte
	wrapSPNEGO      bool

	mu        sync.Mutex
	type1Addr string
	type3Addr string
	type3Seen bool
	postBody  []byte
}

func newNTLMTestServer(t *testing.T, scheme, password string) *ntlmTestServer {
	return &ntlmTestServer{
		t:               t,
		password:        password,
		scheme:          scheme,
		offer:           []string{scheme},
		serverChallenge: []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
		targetInfo:      ntlmTargetInfoWithTimestamp(),
		wrapSPNEGO:      strings.EqualFold(scheme, "Negotiate"),
	}
}

func ntlmTargetInfoWithTimestamp() []byte {
	info := appendAVPair(nil, 0x0002, utf16le("CORP"))
	info = appendAVPair(info, avTimestamp, windowsTimestamp(nowFunc()))
	return appendAVPair(info, avEOL, nil)
}

func (s *ntlmTestServer) challengeToken() []byte {
	msg := buildTestChallenge(s.t, flagNegotiateTargetInfo|flagNegotiateUnicode|flagNegotiateKeyExchange, s.targetInfo)
	if s.wrapSPNEGO {
		return spnegoNegTokenResp(msg)
	}
	return msg
}

func (s *ntlmTestServer) handle(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		for _, scheme := range s.offer {
			w.Header().Add("WWW-Authenticate", scheme)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	_, rawToken, _ := strings.Cut(auth, " ")
	token, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rawToken))
	if err != nil {
		s.t.Errorf("authorization token is not base64: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ntlm := token
	if s.wrapSPNEGO {
		if ntlm, err = spnegoExtractNTLM(token); err != nil {
			s.t.Errorf("server could not extract NTLM from SPNEGO: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	switch messageType(ntlm) {
	case ntlmNegotiate:
		s.mu.Lock()
		s.type1Addr = r.RemoteAddr
		s.mu.Unlock()
		w.Header().Set("WWW-Authenticate", s.headerScheme()+" "+base64.StdEncoding.EncodeToString(s.challengeToken()))
		w.WriteHeader(http.StatusUnauthorized)
	case ntlmAuthenticate:
		s.completeAuthenticate(w, r, ntlm)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (s *ntlmTestServer) headerScheme() string {
	if s.wrapSPNEGO {
		return "Negotiate"
	}
	return "NTLM"
}

func (s *ntlmTestServer) completeAuthenticate(w http.ResponseWriter, r *http.Request, ntlm []byte) {
	s.mu.Lock()
	s.type3Addr = r.RemoteAddr
	s.type3Seen = true
	s.mu.Unlock()
	if !s.verifyProof(ntlm) {
		for _, scheme := range s.offer {
			w.Header().Add("WWW-Authenticate", scheme)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
	case http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.postBody = body
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		_, _ = w.Write(fetchV1ResponseBody("NAK", fakePack(7)))
	}
}

func (s *ntlmTestServer) verifyProof(msg []byte) bool {
	if len(msg) < 64 {
		return false
	}
	field := func(off int) []byte {
		length := int(uint16(msg[off]) | uint16(msg[off+1])<<8)
		start := int(uint32(msg[off+4]) | uint32(msg[off+5])<<8 | uint32(msg[off+6])<<16 | uint32(msg[off+7])<<24)
		if start+length > len(msg) {
			return nil
		}
		return msg[start : start+length]
	}
	ntResponse := field(20)
	domain := utf16Decode(field(28))
	user := utf16Decode(field(36))
	if len(ntResponse) < 16 {
		return false
	}
	proof := ntResponse[:16]
	temp := ntResponse[16:]
	ntowf := ntowfV2(user, domain, []byte(s.password))
	want := hmacMD5(ntowf, append(append([]byte(nil), s.serverChallenge...), temp...))
	return string(proof) == string(want)
}

func messageType(msg []byte) uint32 {
	if len(msg) < 12 || string(msg[:8]) != string(ntlmSignature) {
		return 0
	}
	return uint32(msg[8]) | uint32(msg[9])<<8 | uint32(msg[10])<<16 | uint32(msg[11])<<24
}

func utf16Decode(b []byte) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
	}
	return string(decodeUTF16(units))
}

func decodeUTF16(units []uint16) []rune {
	out := make([]rune, 0, len(units))
	for _, u := range units {
		out = append(out, rune(u))
	}
	return out
}

func (s *ntlmTestServer) sameConnection() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.type1Addr != "" && s.type1Addr == s.type3Addr
}

func TestHTTPNTLMHandshakeAuthenticatesAndReusesTheConnection(t *testing.T) {
	server := httptest.NewServer(nil)
	handler := newNTLMTestServer(t, "NTLM", "hunter2")
	server.Config.Handler = http.HandlerFunc(handler.handle)
	t.Cleanup(server.Close)

	creds := &fakeCredentialSource{creds: []Credentials{{Username: `CORP\alice`, Password: []byte("hunter2")}}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if len(creds.retries) != 1 {
		t.Fatalf("credential source called %d times, want 1", len(creds.retries))
	}
	if !handler.type3Seen {
		t.Fatalf("server never received the NTLM authenticate message")
	}
	if !handler.sameConnection() {
		t.Fatalf("type1 %q and type3 %q arrived on different connections", handler.type1Addr, handler.type3Addr)
	}
}

func TestHTTPNegotiateHandshakeWrapsNTLMInSPNEGO(t *testing.T) {
	server := httptest.NewServer(nil)
	handler := newNTLMTestServer(t, "Negotiate", "s3cret")
	server.Config.Handler = http.HandlerFunc(handler.handle)
	t.Cleanup(server.Close)

	creds := &fakeCredentialSource{creds: []Credentials{{Username: "alice@corp", Password: []byte("s3cret")}}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if !handler.type3Seen {
		t.Fatalf("server never received the Negotiate authenticate message")
	}
}

func TestHTTPNTLMHandshakeReplaysThePostBodyForFetch(t *testing.T) {
	server := httptest.NewServer(nil)
	handler := newNTLMTestServer(t, "NTLM", "hunter2")
	server.Config.Handler = http.HandlerFunc(handler.handle)
	t.Cleanup(server.Close)

	creds := &fakeCredentialSource{creds: []Credentials{{Username: `CORP\alice`, Password: []byte("hunter2")}}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	resp, err := session.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{idOf(1)}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	t.Cleanup(func() { _ = resp.Pack.Close() })
	if _, err := io.ReadAll(resp.Pack); err != nil {
		t.Fatalf("reading pack returned error %v", err)
	}
	handler.mu.Lock()
	body := string(handler.postBody)
	handler.mu.Unlock()
	if !strings.Contains(body, "want "+idOf(1).String()) {
		t.Fatalf("authenticated POST body = %q, missing the want line", body)
	}
}

func TestHTTPNTLMHandshakeRetriesAfterAWrongPassword(t *testing.T) {
	server := httptest.NewServer(nil)
	handler := newNTLMTestServer(t, "NTLM", "correcthorse")
	server.Config.Handler = http.HandlerFunc(handler.handle)
	t.Cleanup(server.Close)

	creds := &fakeCredentialSource{creds: []Credentials{
		{Username: `CORP\alice`, Password: []byte("wrong")},
		{Username: `CORP\alice`, Password: []byte("stillwrong")},
	}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	_, err = session.Advertise(t.Context())
	if !errors.Is(err, ErrNTLMAuthFailed) {
		t.Fatalf("Advertise returned %v, want ErrNTLMAuthFailed", err)
	}
	if len(creds.retries) != 2 || creds.retries[0] || !creds.retries[1] {
		t.Fatalf("credential retries = %v, want [false true]", creds.retries)
	}
}

func TestHTTPPrefersNegotiateWhenSeveralSchemesAreOffered(t *testing.T) {
	server := httptest.NewServer(nil)
	handler := newNTLMTestServer(t, "Negotiate", "s3cret")
	handler.offer = []string{"Negotiate", "NTLM", `Basic realm="git"`}
	server.Config.Handler = http.HandlerFunc(handler.handle)
	t.Cleanup(server.Close)

	creds := &fakeCredentialSource{creds: []Credentials{{Username: "alice", Password: []byte("s3cret")}}}
	session, err := Dial(t.Context(), server.URL+"/repo.git", UploadPack, Options{Credentials: creds, Version: 1})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.Advertise(t.Context()); err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if !handler.type3Seen {
		t.Fatalf("server never completed the Negotiate handshake, so a lower-preference scheme was chosen")
	}
}
