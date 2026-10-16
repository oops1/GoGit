package transport

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func startAuthenticatingProxy(t *testing.T, authority *ntlmTestServer, secure bool) *forwardProxy {
	t.Helper()
	p := &forwardProxy{authority: authority}
	p.server = httptest.NewUnstartedServer(http.HandlerFunc(p.serve))
	p.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	if secure {
		p.server.StartTLS()
	} else {
		p.server.Start()
	}
	t.Cleanup(func() {
		p.server.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, c := range p.conns {
			_ = c.Close()
		}
	})
	return p
}

type proxyTarget struct {
	url    string
	config string
}

func newProxyTarget(t *testing.T, secure bool) proxyTarget {
	t.Helper()
	handler := http.HandlerFunc(advertiseV1Handler)
	if !secure {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		return proxyTarget{url: server.URL}
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return proxyTarget{url: server.URL, config: "\tsslCAInfo = " + writeServerCertificate(t, server) + "\n"}
}

func proxyConfig(t *testing.T, proxyURL string, target proxyTarget, extra string) Options {
	t.Helper()
	clearProxyEnvironment(t)
	unsetProxyAuthMethodEnvironment(t)
	cfg := testGitConfig(t, "[http]\n\tproxy = "+proxyURL+"\n\temptyAuth = false\n"+target.config+extra)
	return Options{Config: cfg}
}

func unsetProxyAuthMethodEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_HTTP_PROXY_AUTHMETHOD", "")
	if err := os.Unsetenv("GIT_HTTP_PROXY_AUTHMETHOD"); err != nil {
		t.Fatalf("Unsetenv returned %v", err)
	}
}

func TestHTTPAuthenticatesToAnNTLMOrNegotiateProxy(t *testing.T) {
	for _, scheme := range []string{"NTLM", "Negotiate"} {
		for _, secure := range []bool{false, true} {
			name := scheme + "/plain"
			if secure {
				name = scheme + "/connect"
			}
			t.Run(name, func(t *testing.T) {
				authority := newNTLMProxyAuthority(t, scheme, "hunter2")
				proxy := startAuthenticatingProxy(t, authority, false)
				target := newProxyTarget(t, secure)
				opts := proxyConfig(t, proxy.server.URL, target, "")
				source := &feedbackRecorder{supply: []Credentials{{Username: `CORP\alice`, Password: []byte("hunter2")}}}
				opts.Credentials = source
				if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
					t.Fatalf("advertise through the %s proxy returned error %v", scheme, err)
				}
				if authority.authenticated == 0 || !authority.sameConnection() {
					t.Fatalf("authenticated %d times, same connection %v; want a completed handshake on one connection", authority.authenticated, authority.sameConnection())
				}
				if len(source.asked) != 1 || source.asked[0] != proxy.server.URL {
					t.Fatalf("asked = %q, want one question about %s", source.asked, proxy.server.URL)
				}
				if len(source.approved) != 1 || !strings.HasPrefix(source.approved[0], proxy.server.URL+` CORP\alice:`) {
					t.Fatalf("approved = %q, want the proxy credential", source.approved)
				}
			})
		}
	}
}

func TestHTTPUsesTheProxyURLCredentialsForNTLMWithoutAsking(t *testing.T) {
	authority := newNTLMProxyAuthority(t, "NTLM", "hunter2")
	proxy := startAuthenticatingProxy(t, authority, false)
	target := newProxyTarget(t, true)
	proxyURL := strings.Replace(proxy.server.URL, "http://", `http://CORP%5Calice:hunter2@`, 1)
	opts := proxyConfig(t, proxyURL, target, "")
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	if authority.authenticated == 0 {
		t.Fatalf("the proxy never authenticated the URL credentials")
	}
}

func TestHTTPGivesUpOnAnNTLMProxyAfterTwoWrongPasswords(t *testing.T) {
	for _, secure := range []bool{false, true} {
		authority := newNTLMProxyAuthority(t, "NTLM", "correcthorse")
		proxy := startAuthenticatingProxy(t, authority, false)
		target := newProxyTarget(t, secure)
		opts := proxyConfig(t, proxy.server.URL, target, "")
		source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("wrong")}}}
		opts.Credentials = source
		if err := advertiseThrough(t, target.url+"/repo.git", opts); !errors.Is(err, ErrProxyAuthRequired) {
			t.Fatalf("secure=%v: advertise returned %v, want ErrProxyAuthRequired", secure, err)
		}
		if len(source.asked) != 2 || len(source.rejected) != 2 || source.retries[0] || !source.retries[1] {
			t.Fatalf("secure=%v: asked %q retries %v rejected %q, want two refused questions", secure, source.asked, source.retries, source.rejected)
		}
	}
}

func TestHTTPHonoursProxyAuthMethod(t *testing.T) {
	authority := newNTLMProxyAuthority(t, "NTLM", "hunter2")
	proxy := startAuthenticatingProxy(t, authority, false)
	target := newProxyTarget(t, true)
	credentials := func() *feedbackRecorder {
		return &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	}

	opts := proxyConfig(t, proxy.server.URL, target, "\tproxyAuthMethod = basic\n")
	source := credentials()
	opts.Credentials = source
	if err := advertiseThrough(t, target.url+"/repo.git", opts); !errors.Is(err, ErrProxyAuthRequired) {
		t.Fatalf("proxyAuthMethod=basic returned %v, want ErrProxyAuthRequired", err)
	}
	if trace := authority.takeTrace(); !slices.Contains(trace, "CONNECT basic other") || slices.ContainsFunc(trace, func(s string) bool { return strings.Contains(s, "ntlm") }) {
		t.Fatalf("proxyAuthMethod=basic sent %q, want Basic only", trace)
	}

	opts = proxyConfig(t, proxy.server.URL, target, "\tproxyAuthMethod = digest\n")
	source = credentials()
	opts.Credentials = source
	if err := advertiseThrough(t, target.url+"/repo.git", opts); !errors.Is(err, ErrProxyAuthRequired) || len(source.asked) != 0 {
		t.Fatalf("proxyAuthMethod=digest returned %v after %d questions, want ErrProxyAuthRequired without asking", err, len(source.asked))
	}
	authority.takeTrace()

	opts = proxyConfig(t, proxy.server.URL, target, "\tproxyAuthMethod = ntlm\n")
	opts.Credentials = credentials()
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
		t.Fatalf("proxyAuthMethod=ntlm returned error %v", err)
	}
	if trace := authority.takeTrace(); !slices.Equal(trace, []string{"CONNECT ntlm type1", "CONNECT ntlm type3"}) {
		t.Fatalf("proxyAuthMethod=ntlm sent %q, want the handshake without a probe", trace)
	}

	opts = proxyConfig(t, proxy.server.URL, target, "\tproxyAuthMethod = something\n")
	opts.Credentials = credentials()
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
		t.Fatalf("an unknown proxyAuthMethod must behave like anyauth, got %v", err)
	}
}

func TestHTTPFallsBackToCredentialsWhenIntegratedProxyAuthIsRejected(t *testing.T) {
	authority := newNTLMProxyAuthority(t, "Negotiate", "hunter2")
	proxy := startAuthenticatingProxy(t, authority, false)
	target := newProxyTarget(t, true)
	clearProxyEnvironment(t)
	unsetProxyAuthMethodEnvironment(t)
	cfg := testGitConfig(t, "[http]\n\tproxy = "+proxy.server.URL+"\n"+target.config)
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	if err := advertiseThrough(t, target.url+"/repo.git", Options{Config: cfg, Credentials: source}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	if len(source.asked) != 1 {
		t.Fatalf("asked = %q, want one question after integrated auth", source.asked)
	}
}

func TestHTTPReopensTheTunnelWhenTheProxyClosesAfter407(t *testing.T) {
	authority := newNTLMProxyAuthority(t, "NTLM", "hunter2")
	target := newProxyTarget(t, true)
	p := &forwardProxy{authority: authority}
	p.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") == "" {
			w.Header().Set("Connection", "close")
		}
		p.serve(w, r)
	}))
	p.server.Start()
	t.Cleanup(p.server.Close)
	opts := proxyConfig(t, p.server.URL, target, "")
	opts.Credentials = &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
}

func TestHTTPReportsAProxyThatRefusesTheTunnel(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	target := newProxyTarget(t, true)
	opts := proxyConfig(t, proxy.URL, target, "")
	if err := advertiseThrough(t, target.url+"/repo.git", opts); !errors.Is(err, errProxyConnectRefused) {
		t.Fatalf("advertise returned %v, want errProxyConnectRefused", err)
	}
}

func TestHTTPReportsAnUnreachableTunnelProxy(t *testing.T) {
	target := newProxyTarget(t, true)
	opts := proxyConfig(t, "http://127.0.0.1:1", target, "")
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err == nil {
		t.Fatalf("advertise through an unreachable proxy succeeded")
	}
}

func TestHTTPBindsNTLMToAnHTTPSProxyCertificate(t *testing.T) {
	for _, secureTarget := range []bool{false, true} {
		authority := newNTLMProxyAuthority(t, "NTLM", "hunter2")
		proxy := startAuthenticatingProxy(t, authority, true)
		if secureTarget {
			authority.expectBinding = ntlmChannelBindingHash(tlsServerEndPoint(proxy.server.Certificate()))
		}
		target := newProxyTarget(t, secureTarget)
		extra := "\tsslCAInfo = " + writeServerCertificate(t, proxy.server) + "\n"
		target.config = ""
		opts := proxyConfig(t, proxy.server.URL, target, extra)
		opts.Credentials = &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
		if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
			t.Fatalf("secureTarget=%v: advertise through an https proxy returned error %v", secureTarget, err)
		}
		if authority.authenticated == 0 {
			t.Fatalf("secureTarget=%v: the https proxy never authenticated", secureTarget)
		}
	}
}

func TestHTTPReportsAnHTTPSProxyWithAnUntrustedCertificate(t *testing.T) {
	authority := newNTLMProxyAuthority(t, "NTLM", "hunter2")
	proxy := startAuthenticatingProxy(t, authority, true)
	target := newProxyTarget(t, true)
	opts := proxyConfig(t, proxy.server.URL, proxyTarget{url: target.url}, "")
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err == nil {
		t.Fatalf("advertise through an untrusted https proxy succeeded")
	}
}

func TestSelectProxySchemeFollowsTheMethod(t *testing.T) {
	all := map[string]string{"negotiate": "", "ntlm": "", "basic": ""}
	cases := []struct {
		method     string
		challenges map[string]string
		want       string
	}{
		{"", all, schemeNegotiate},
		{"anyauth", map[string]string{"ntlm": "", "basic": ""}, schemeNTLM},
		{"anyauth", map[string]string{}, schemeBasic},
		{"anyauth", map[string]string{"digest": ""}, ""},
		{"basic", all, schemeBasic},
		{"ntlm", map[string]string{}, schemeNTLM},
		{"digest", all, ""},
	}
	for _, c := range cases {
		if got := selectProxyScheme(c.challenges, c.method); got != c.want {
			t.Errorf("selectProxyScheme(%v, %q) = %q, want %q", c.challenges, c.method, got, c.want)
		}
	}
}

func TestPlainProxyForIgnoresForeignTransportsAndBadURLs(t *testing.T) {
	clearProxyEnvironment(t)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")
	s := testAuthSession(t, Options{})
	if s.plainProxyFor("http://git.example.com/repo.git") == nil {
		t.Fatalf("plain http target should go through the proxy")
	}
	if s.plainProxyFor("https://git.example.com/repo.git") != nil {
		t.Fatalf("https target must be tunnelled, not proxied in plain")
	}
	if s.plainProxyFor("http://\x00") != nil {
		t.Fatalf("an unparsable target must not be proxied")
	}
	s.ownsTransport = false
	if s.plainProxyFor("http://git.example.com/repo.git") != nil {
		t.Fatalf("a caller-supplied client must not be proxied by the session")
	}
}
