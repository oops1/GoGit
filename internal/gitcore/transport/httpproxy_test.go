package transport

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type forwardProxy struct {
	server   *httptest.Server
	auth     string
	requests atomic.Int32
	mu       sync.Mutex
	conns    []net.Conn
}

func newForwardProxy(t *testing.T, user, password string) *forwardProxy {
	t.Helper()
	p := &forwardProxy{}
	if user != "" {
		p.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
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

func (p *forwardProxy) track(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns = append(p.conns, conns...)
}

func (p *forwardProxy) serve(w http.ResponseWriter, r *http.Request) {
	p.requests.Add(1)
	if p.auth != "" && r.Header.Get("Proxy-Authorization") != p.auth {
		w.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Authorization")
	direct := &http.Transport{}
	defer direct.CloseIdleConnections()
	resp, err := direct.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *forwardProxy) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	client, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	p.track(client, upstream)
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	go func() { _, _ = io.Copy(upstream, buffered) }()
	go func() { _, _ = io.Copy(client, upstream) }()
}

func readExactly(conn net.Conn, n int) ([]byte, bool) {
	buf := make([]byte, n)
	_, err := io.ReadFull(conn, buf)
	return buf, err == nil
}

func socks5Authenticate(conn net.Conn, user, password string) bool {
	head, ok := readExactly(conn, 2)
	if !ok {
		return false
	}
	name, ok := readExactly(conn, int(head[1]))
	if !ok {
		return false
	}
	size, ok := readExactly(conn, 1)
	if !ok {
		return false
	}
	secret, ok := readExactly(conn, int(size[0]))
	if !ok {
		return false
	}
	accepted := string(name) == user && string(secret) == password
	status := byte(1)
	if accepted {
		status = 0
	}
	_, _ = conn.Write([]byte{1, status})
	return accepted
}

func socks5Handshake(conn net.Conn, user, password string) (net.Conn, bool) {
	head, ok := readExactly(conn, 2)
	if !ok || head[0] != 5 {
		return nil, false
	}
	if _, ok := readExactly(conn, int(head[1])); !ok {
		return nil, false
	}
	if user == "" {
		_, _ = conn.Write([]byte{5, 0})
	} else {
		_, _ = conn.Write([]byte{5, 2})
		if !socks5Authenticate(conn, user, password) {
			return nil, false
		}
	}
	request, ok := readExactly(conn, 4)
	if !ok || request[1] != 1 {
		return nil, false
	}
	var host string
	switch request[3] {
	case 1:
		addr, ok := readExactly(conn, 4)
		if !ok {
			return nil, false
		}
		host = net.IP(addr).String()
	case 3:
		size, ok := readExactly(conn, 1)
		if !ok {
			return nil, false
		}
		name, ok := readExactly(conn, int(size[0]))
		if !ok {
			return nil, false
		}
		host = string(name)
	default:
		return nil, false
	}
	port, ok := readExactly(conn, 2)
	if !ok {
		return nil, false
	}
	upstream, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))))
	if err != nil {
		_, _ = conn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return nil, false
	}
	_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	return upstream, true
}

func startSocks5Proxy(t *testing.T, user, password string) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	track := func(c net.Conn) {
		mu.Lock()
		defer mu.Unlock()
		conns = append(conns, c)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	tunnels := new(atomic.Int32)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			track(conn)
			go func() {
				upstream, ok := socks5Handshake(conn, user, password)
				if !ok {
					_ = conn.Close()
					return
				}
				tunnels.Add(1)
				track(upstream)
				go func() { _, _ = io.Copy(upstream, conn) }()
				_, _ = io.Copy(conn, upstream)
			}()
		}
	}()
	return listener.Addr().String(), tunnels
}

func TestParseProxyURLNormalizesLikeGit(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"proxy.local:3128", "http://proxy.local:3128"},
		{"proxy.local", "http://proxy.local:1080"},
		{"HTTPS://proxy.local", "https://proxy.local:443"},
		{"socks5h://alice:secret@proxy.local:9050", "socks5h://alice:secret@proxy.local:9050"},
		{"socks5://proxy.local", "socks5://proxy.local:1080"},
		{"[::1]:8080", "http://[::1]:8080"},
	}
	for _, tt := range tests {
		got, err := parseProxyURL(tt.raw)
		if err != nil {
			t.Fatalf("parseProxyURL(%q) returned error %v", tt.raw, err)
		}
		if got.String() != tt.want {
			t.Errorf("parseProxyURL(%q) = %q, want %q", tt.raw, got.String(), tt.want)
		}
	}
}

func TestParseProxyURLRejectsUnsupportedAddressesWithoutEchoingSecrets(t *testing.T) {
	for _, raw := range []string{"socks4://proxy.local", "http://", "http://user:secret@%zz", "ftp://user:secret@proxy.local"} {
		_, err := parseProxyURL(raw)
		if !errors.Is(err, ErrInvalidProxy) {
			t.Fatalf("parseProxyURL(%q) returned %v, want ErrInvalidProxy", raw, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("parseProxyURL(%q) error %q reveals the password", raw, err)
		}
	}
}

func TestNoProxyMatchesHostsDomainsAndNetworks(t *testing.T) {
	tests := []struct {
		list string
		host string
		want bool
	}{
		{"", "example.com", false},
		{"*", "example.com", true},
		{"example.com", "example.com", true},
		{"example.com", "git.example.com", true},
		{".example.com", "git.example.com", true},
		{"example.com", "badexample.com", false},
		{"10.0.0.0/8", "10.1.2.3", true},
		{"10.0.0.0/8", "11.1.2.3", false},
		{"10.0.0.0/8", "example.com", false},
		{"example.com:8080", "example.com", true},
		{"[::1]", "::1", true},
		{"a.com, B.com", "b.com", true},
	}
	for _, tt := range tests {
		if got := noProxyMatches(tt.list, tt.host); got != tt.want {
			t.Errorf("noProxyMatches(%q, %q) = %v, want %v", tt.list, tt.host, got, tt.want)
		}
	}
}

func TestProxySelectorReadsTheSchemeSpecificEnvironment(t *testing.T) {
	clearProxyEnvironment(t)
	t.Setenv("HTTP_PROXY", "plain:1")
	t.Setenv("HTTPS_PROXY", "secure:1")
	t.Setenv("ALL_PROXY", "any:1")
	selector := newProxySelector(httpSettings{})
	if got := selector.rawFor("https"); got != "secure:1" {
		t.Fatalf("https proxy = %q, want secure:1", got)
	}
	if got := selector.rawFor("http"); got != "plain:1" {
		t.Fatalf("http proxy = %q, want plain:1", got)
	}
	t.Setenv("HTTPS_PROXY", "")
	if got := newProxySelector(httpSettings{}).rawFor("https"); got != "any:1" {
		t.Fatalf("https proxy without HTTPS_PROXY = %q, want any:1", got)
	}
	if got := newProxySelector(httpSettings{proxy: "cfg:1", proxySet: true}).rawFor("https"); got != "cfg:1" {
		t.Fatalf("configured proxy = %q, want cfg:1", got)
	}
}

func TestProxySelectorUsesSuppliedCredentialsAndReportsTheProxyResource(t *testing.T) {
	selector := newProxySelector(httpSettings{proxy: "http://embedded:old@proxy.local:3128", proxySet: true})
	req, err := http.NewRequest(http.MethodGet, "https://git.example.com/repo.git", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error %v", err)
	}
	if got := selector.resource(); got != "" {
		t.Fatalf("resource before any request = %q, want empty", got)
	}
	selector.authenticate(url.UserPassword("alice", "secret"))
	proxyURL, err := selector.proxy(req)
	if err != nil {
		t.Fatalf("proxy returned error %v", err)
	}
	if proxyURL.User.String() != "alice:secret" {
		t.Fatalf("proxy user = %q, want the supplied credentials", proxyURL.User.String())
	}
	if got := selector.resource(); got != "http://proxy.local:3128" {
		t.Fatalf("resource = %q, want http://proxy.local:3128", got)
	}
	disabled := newProxySelector(httpSettings{proxySet: true})
	if proxyURL, err := disabled.proxy(req); proxyURL != nil || err != nil {
		t.Fatalf("empty configured proxy gave %v, %v; want no proxy", proxyURL, err)
	}
	broken := newProxySelector(httpSettings{proxy: "socks4://proxy.local", proxySet: true})
	if _, err := broken.proxy(req); !errors.Is(err, ErrInvalidProxy) {
		t.Fatalf("invalid proxy gave %v, want ErrInvalidProxy", err)
	}
}

func TestHTTPReachesAnHTTPSRemoteThroughTheEnvironmentProxy(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(advertiseV1Handler))
	t.Cleanup(target.Close)
	caFile := writeServerCertificate(t, target)
	clearProxyEnvironment(t)
	proxy := newForwardProxy(t, "", "")
	t.Setenv("HTTPS_PROXY", proxy.server.URL)
	cfg := testGitConfig(t, "[http]\n\tsslCAInfo = "+caFile+"\n")
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg}); err != nil {
		t.Fatalf("advertise through the proxy returned error %v", err)
	}
	if proxy.requests.Load() == 0 {
		t.Fatalf("the proxy saw no CONNECT request")
	}
}

func TestHTTPSendsTheProxyURLCredentialsToTheProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(advertiseV1Handler))
	t.Cleanup(target.Close)
	clearProxyEnvironment(t)
	proxy := newForwardProxy(t, "alice", "secret")
	proxyURL := strings.Replace(proxy.server.URL, "http://", "http://alice:secret@", 1)
	cfg := testGitConfig(t, "[http]\n\tproxy = "+proxyURL+"\n")
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg}); err != nil {
		t.Fatalf("advertise through the proxy returned error %v", err)
	}
	if proxy.requests.Load() != 1 {
		t.Fatalf("proxy requests = %d, want 1", proxy.requests.Load())
	}
}

func TestHTTPAsksTheCredentialSourceWhenTheProxyAnswers407(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run("tls="+strconv.FormatBool(secure), func(t *testing.T) {
			handler := http.HandlerFunc(advertiseV1Handler)
			target := httptest.NewServer(handler)
			config := ""
			if secure {
				target.Close()
				target = httptest.NewTLSServer(handler)
				config = "\tsslCAInfo = " + writeServerCertificate(t, target) + "\n"
			}
			t.Cleanup(target.Close)
			clearProxyEnvironment(t)
			proxy := newForwardProxy(t, "alice", "secret")
			cfg := testGitConfig(t, "[http]\n\tproxy = "+proxy.server.URL+"\n"+config)
			source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("secret")}}}
			if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg, Credentials: source}); err != nil {
				t.Fatalf("advertise returned error %v", err)
			}
			if len(source.asked) != 1 || source.asked[0] != proxy.server.URL {
				t.Fatalf("asked = %q, want one question about %s", source.asked, proxy.server.URL)
			}
			if len(source.approved) != 1 || !strings.HasPrefix(source.approved[0], proxy.server.URL+" alice:") {
				t.Fatalf("approved = %q, want the proxy credential", source.approved)
			}
		})
	}
}

func TestHTTPGivesUpOnTheProxyAfterTwoRefusedCredentials(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(advertiseV1Handler))
	t.Cleanup(target.Close)
	clearProxyEnvironment(t)
	proxy := newForwardProxy(t, "alice", "secret")
	cfg := testGitConfig(t, "[http]\n\tproxy = "+proxy.server.URL+"\n")
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("wrong")}}}
	err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg, Credentials: source})
	if !errors.Is(err, ErrProxyAuthRequired) {
		t.Fatalf("advertise returned %v, want ErrProxyAuthRequired", err)
	}
	if len(source.asked) != 2 || len(source.rejected) != 2 || source.retries[0] || !source.retries[1] {
		t.Fatalf("asked %q retries %v rejected %q, want two refused questions", source.asked, source.retries, source.rejected)
	}
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg}); !errors.Is(err, ErrProxyAuthRequired) {
		t.Fatalf("advertise without a credential source returned %v, want ErrProxyAuthRequired", err)
	}
	boom := errors.New("boom")
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg, Credentials: &fakeCredentialSource{err: boom}}); !errors.Is(err, boom) {
		t.Fatalf("advertise with a failing credential source returned %v, want boom", err)
	}
}

func TestHTTPBypassesTheProxyForNoProxyHostsAndAnEmptyRemoteProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(advertiseV1Handler))
	t.Cleanup(target.Close)
	clearProxyEnvironment(t)
	proxy := newForwardProxy(t, "", "")
	t.Setenv("HTTP_PROXY", proxy.server.URL)
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	t.Setenv("NO_PROXY", "")
	cfg := testGitConfig(t, "[remote \"origin\"]\n\tproxy =\n")
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg, RemoteName: "origin"}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	if proxy.requests.Load() != 0 {
		t.Fatalf("proxy requests = %d, want the proxy bypassed", proxy.requests.Load())
	}
}

func TestHTTPReachesTheRemoteThroughASocks5Proxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(advertiseV1Handler))
	t.Cleanup(target.Close)
	clearProxyEnvironment(t)
	addr, tunnels := startSocks5Proxy(t, "alice", "secret")
	t.Setenv("ALL_PROXY", "socks5h://alice:secret@"+addr)
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{}); err != nil {
		t.Fatalf("advertise through socks5 returned error %v", err)
	}
	if tunnels.Load() == 0 {
		t.Fatalf("the socks5 proxy opened no tunnel")
	}
	anonymous, _ := startSocks5Proxy(t, "", "")
	t.Setenv("ALL_PROXY", "socks5://"+anonymous)
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{}); err != nil {
		t.Fatalf("advertise through anonymous socks5 returned error %v", err)
	}
}

func TestHTTPReportsAnUnsupportedProxy(t *testing.T) {
	clearProxyEnvironment(t)
	cfg := testGitConfig(t, "[http]\n\tproxy = socks4://127.0.0.1:1\n")
	if err := advertiseThrough(t, "http://127.0.0.1:1/repo.git", Options{Config: cfg}); !errors.Is(err, ErrInvalidProxy) {
		t.Fatalf("advertise returned %v, want ErrInvalidProxy", err)
	}
}

func TestHTTPSendsAProxyTokenAsThePassword(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(advertiseV1Handler))
	t.Cleanup(target.Close)
	clearProxyEnvironment(t)
	proxy := newForwardProxy(t, "alice", "token-value")
	cfg := testGitConfig(t, "[http]\n\tproxy = "+proxy.server.URL+"\n")
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Token: []byte("token-value")}}}
	if err := advertiseThrough(t, target.URL+"/repo.git", Options{Config: cfg, Credentials: source}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
}

func TestResolveHTTPSettingsMatchesPlainHTTPOnTheDefaultPort(t *testing.T) {
	cfg := testGitConfig(t, "[http \"http://git.example.com\"]\n\tproxy = plain:1\n")
	s, err := resolveHTTPSettings(cfg, "", Endpoint{Scheme: SchemeHTTP, Host: "git.example.com", Port: "80", Path: "/repo.git"})
	if err != nil || s.proxy != "plain:1" {
		t.Fatalf("resolveHTTPSettings gave proxy %q, %v; want plain:1", s.proxy, err)
	}
}

func TestRejectProxyChallengeOnlyStopsOn407(t *testing.T) {
	if err := rejectProxyChallenge(t.Context(), nil, nil, &http.Response{StatusCode: http.StatusOK}); err != nil {
		t.Fatalf("rejectProxyChallenge(200) = %v, want nil", err)
	}
	if err := rejectProxyChallenge(t.Context(), nil, nil, &http.Response{StatusCode: http.StatusProxyAuthRequired}); !errors.Is(err, errProxyChallenge) {
		t.Fatalf("rejectProxyChallenge(407) = %v, want errProxyChallenge", err)
	}
}
