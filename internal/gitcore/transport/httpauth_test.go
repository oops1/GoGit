package transport

import (
	"encoding/base64"
	"encoding/binary"
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
	offer           []string
	serverChallenge []byte
	targetInfo      []byte
	wrapSPNEGO      bool
	requestHeader   string
	challengeHeader string
	status          int
	expectBinding   []byte

	mu            sync.Mutex
	type1Addr     string
	type3Addr     string
	type3Seen     bool
	authenticated int
	sawBinding    []byte
	postBody      []byte
	trace         []string
}

func (s *ntlmTestServer) record(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trace = append(s.trace, step)
}

func (s *ntlmTestServer) takeTrace() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	trace := s.trace
	s.trace = nil
	return trace
}

func newNTLMTestServer(t *testing.T, scheme, password string) *ntlmTestServer {
	return &ntlmTestServer{
		t:               t,
		password:        password,
		offer:           []string{scheme},
		serverChallenge: []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
		targetInfo:      ntlmTargetInfoWithTimestamp(),
		wrapSPNEGO:      strings.EqualFold(scheme, "Negotiate"),
		requestHeader:   "Authorization",
		challengeHeader: "WWW-Authenticate",
		status:          http.StatusUnauthorized,
	}
}

func newNTLMProxyAuthority(t *testing.T, scheme, password string) *ntlmTestServer {
	s := newNTLMTestServer(t, scheme, password)
	s.requestHeader = "Proxy-Authorization"
	s.challengeHeader = "Proxy-Authenticate"
	s.status = http.StatusProxyAuthRequired
	return s
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

func (s *ntlmTestServer) reject(w http.ResponseWriter) {
	for _, scheme := range s.offer {
		w.Header().Add(s.challengeHeader, scheme)
	}
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(s.status)
}

func (s *ntlmTestServer) headerScheme() string {
	if s.wrapSPNEGO {
		return "Negotiate"
	}
	return "NTLM"
}

func (s *ntlmTestServer) authorize(w http.ResponseWriter, r *http.Request) bool {
	auth := r.Header.Get(s.requestHeader)
	if auth == "" {
		s.record(r.Method + " none")
		s.reject(w)
		return false
	}
	name, rawToken, _ := strings.Cut(auth, " ")
	token, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rawToken))
	s.record(r.Method + " " + strings.ToLower(name) + " " + ntlmStepName(token))
	if err != nil {
		s.reject(w)
		return false
	}
	ntlm := token
	if s.wrapSPNEGO {
		if ntlm, err = spnegoExtractNTLM(token); err != nil {
			s.reject(w)
			return false
		}
	}
	switch messageType(ntlm) {
	case ntlmNegotiate:
		s.mu.Lock()
		s.type1Addr = r.RemoteAddr
		s.mu.Unlock()
		w.Header().Set(s.challengeHeader, s.headerScheme()+" "+base64.StdEncoding.EncodeToString(s.challengeToken()))
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(s.status)
		return false
	case ntlmAuthenticate:
		s.mu.Lock()
		s.type3Addr = r.RemoteAddr
		s.type3Seen = true
		s.mu.Unlock()
		if !s.verifyProof(ntlm) {
			s.reject(w)
			return false
		}
		s.mu.Lock()
		s.authenticated++
		s.mu.Unlock()
		return true
	default:
		s.reject(w)
		return false
	}
}

func (s *ntlmTestServer) handle(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
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

func ntlmMessageField(msg []byte, off int) []byte {
	length := int(binary.LittleEndian.Uint16(msg[off:]))
	start := int(binary.LittleEndian.Uint32(msg[off+4:]))
	if start+length > len(msg) {
		return nil
	}
	return msg[start : start+length]
}

func avPairValue(info []byte, want uint16) []byte {
	for len(info) >= 4 {
		id := binary.LittleEndian.Uint16(info)
		length := int(binary.LittleEndian.Uint16(info[2:]))
		if id == avEOL || 4+length > len(info) {
			return nil
		}
		if id == want {
			return info[4 : 4+length]
		}
		info = info[4+length:]
	}
	return nil
}

func (s *ntlmTestServer) verifyProof(msg []byte) bool {
	if len(msg) < 64 {
		return false
	}
	ntResponse := ntlmMessageField(msg, 20)
	domain := utf16Decode(ntlmMessageField(msg, 28))
	user := utf16Decode(ntlmMessageField(msg, 36))
	if len(ntResponse) < 16+28 {
		return false
	}
	proof := ntResponse[:16]
	temp := ntResponse[16:]
	binding := avPairValue(temp[28:], avChannelBind)
	s.mu.Lock()
	s.sawBinding = append([]byte(nil), binding...)
	s.mu.Unlock()
	if s.expectBinding != nil && string(binding) != string(s.expectBinding) {
		return false
	}
	ntowf := ntowfV2(user, domain, []byte(s.password))
	want := hmacMD5(ntowf, append(append([]byte(nil), s.serverChallenge...), temp...))
	return string(proof) == string(want)
}

func ntlmStepName(token []byte) string {
	if inner, err := spnegoExtractNTLM(token); err == nil {
		token = inner
	}
	switch messageType(token) {
	case ntlmNegotiate:
		return "type1"
	case ntlmAuthenticate:
		return "type3"
	default:
		return "other"
	}
}

func messageType(msg []byte) uint32 {
	if len(msg) < 12 || string(msg[:8]) != string(ntlmSignature) {
		return 0
	}
	return binary.LittleEndian.Uint32(msg[8:])
}

func utf16Decode(b []byte) string {
	runes := make([]rune, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		runes = append(runes, rune(binary.LittleEndian.Uint16(b[i:])))
	}
	return string(runes)
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
