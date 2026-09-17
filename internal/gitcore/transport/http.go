package transport

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/gitcore/progress"
)

const defaultUserAgent = "git/2.45.0 (Go.Git)"

const tlsHandshakeTimeout = 15 * time.Second

var (
	discoveryHeaderTimeout  = 30 * time.Second
	errUnsupportedMediaType = errors.New("transport: the server refused the request encoding")
)

func (s *httpSession) buildClient() error {
	tlsConfig, err := buildTLSConfig(s.settings)
	if err != nil {
		return err
	}
	s.tlsConfig = tlsConfig
	s.dialer = &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	s.ownsTransport = true
	s.client = &http.Client{
		Transport: &http.Transport{
			Proxy:                 s.proxy.transportProxy,
			DialContext:           s.dialer.DialContext,
			DialTLSContext:        s.dialTLS,
			TLSClientConfig:       tlsConfig,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ExpectContinueTimeout: 5 * time.Second,
		},
	}
	return nil
}

func httpBaseURL(e Endpoint) string {
	host := e.Host
	if e.Port != "" {
		host = net.JoinHostPort(e.Host, e.Port)
	}
	return string(e.Scheme) + "://" + host + strings.TrimSuffix(e.Path, "/")
}

func hasMediaType(header, want string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil && !errors.Is(err, mime.ErrInvalidMediaParameter) {
		return false
	}
	return strings.EqualFold(mediaType, want)
}

type httpSession struct {
	mu                  sync.Mutex
	endpoint            Endpoint
	password            Password
	service             Service
	opts                Options
	settings            httpSettings
	proxy               *proxySelector
	client              *http.Client
	tlsConfig           *tls.Config
	dialer              *net.Dialer
	ownsTransport       bool
	baseURL             string
	resource            string
	advertised          bool
	version             int
	caps                Capabilities
	authHeader          string
	supplied            *suppliedCredentials
	credAttempts        int
	integratedAttempted bool
	attemptedScheme     string
	proxyMu             sync.Mutex
	proxySupplied       *suppliedCredentials
	proxyAttempts       int
	plainRequests       bool
	closed              bool
}

type suppliedCredentials struct {
	resource string
	creds    Credentials
	approved bool
}

func newHTTPSession(endpoint Endpoint, password Password, service Service, opts Options) (*httpSession, error) {
	settings, err := resolveHTTPSettings(opts.Config, opts.RemoteName, endpoint)
	if err != nil {
		password.Wipe()
		return nil, err
	}
	s := &httpSession{
		endpoint: endpoint,
		password: password,
		service:  service,
		opts:     opts,
		settings: settings,
		proxy:    newProxySelector(settings),
		client:   opts.HTTPClient,
		baseURL:  httpBaseURL(endpoint),
		resource: CredentialResource(endpoint),
	}
	if s.client == nil {
		if err := s.buildClient(); err != nil {
			password.Wipe()
			return nil, err
		}
		if !settings.sslVerify && endpoint.Scheme == SchemeHTTPS {
			opts.Progress.Phase(progress.PhaseTLSVerifyDisabled)
		}
	}
	return s, nil
}

func (s *httpSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.password.Wipe()
	s.authHeader = ""
	forgetCredential(&s.supplied)
	s.proxyMu.Lock()
	forgetCredential(&s.proxySupplied)
	s.proxyMu.Unlock()
	return nil
}

func forgetCredential(slot **suppliedCredentials) {
	if *slot == nil {
		return
	}
	(*slot).creds.Wipe()
	*slot = nil
}

func (s *httpSession) approveCredential(ctx context.Context, supplied *suppliedCredentials) {
	if supplied == nil || supplied.approved {
		return
	}
	supplied.approved = true
	if feedback, ok := s.opts.Credentials.(CredentialFeedback); ok {
		feedback.Approve(ctx, supplied.resource, supplied.creds)
	}
}

func (s *httpSession) rejectCredential(ctx context.Context, slot **suppliedCredentials) {
	if *slot == nil {
		return
	}
	if feedback, ok := s.opts.Credentials.(CredentialFeedback); ok {
		feedback.Reject(ctx, (*slot).resource, (*slot).creds)
	}
	forgetCredential(slot)
}

func (s *httpSession) applyAuthHeader(req *http.Request) {
	if s.authHeader != "" {
		req.Header.Set("Authorization", s.authHeader)
		return
	}
	if s.endpoint.User != "" || len(s.password) > 0 {
		req.SetBasicAuth(s.endpoint.User, string(s.password))
	}
}

func (s *httpSession) authenticate(ctx context.Context) error {
	retry := s.credAttempts > 0
	creds, err := s.opts.Credentials.Credentials(ctx, s.resource, retry)
	s.credAttempts++
	if err != nil {
		return err
	}
	if len(creds.Token) > 0 {
		s.authHeader = "Bearer " + string(creds.Token)
	} else {
		s.authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(creds.Username+":"+string(creds.Password)))
	}
	s.supplied = &suppliedCredentials{resource: s.resource, creds: creds}
	return nil
}

func (s *httpSession) applyHeaders(req *http.Request, contentType, proxyAuthorization string, headers map[string]string) {
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", cmp.Or(s.opts.UserAgent, defaultUserAgent))
	for _, line := range s.settings.extraHeaders {
		if name, value, ok := strings.Cut(line, ":"); ok {
			req.Header.Add(strings.TrimSpace(name), strings.TrimSpace(value))
		}
	}
	s.applyAuthHeader(req)
	if proxyAuthorization != "" {
		req.Header.Set("Proxy-Authorization", proxyAuthorization)
	}
}

func (s *httpSession) attempt(ctx context.Context, method, rawURL, contentType, proxyAuthorization string, body *requestBody, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithCancelCause(ctx)
	req = req.WithContext(reqCtx)
	watch := startLowSpeedWatch(s.settings.lowSpeedLimit, s.settings.lowSpeedTime, cancel)
	if body != nil {
		if err := attachBody(req, body, watch); err != nil {
			watch.stop()
			cancel(nil)
			return nil, err
		}
	}
	s.applyHeaders(req, contentType, proxyAuthorization, headers)
	stopHeaderTimer := func() bool { return false }
	if method == http.MethodGet {
		stopHeaderTimer = time.AfterFunc(discoveryHeaderTimeout, func() { cancel(ErrHeaderTimeout) }).Stop
	}
	resp, err := s.client.Do(req)
	stopHeaderTimer()
	if err != nil {
		watch.stop()
		cause := context.Cause(reqCtx)
		cancel(nil)
		return nil, transferError(err, cause)
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, watch: watch, ctx: reqCtx, cancel: cancel}
	return resp, nil
}

func (s *httpSession) send(ctx context.Context, method, suffix, contentType string, body *requestBody, headers map[string]string) (*http.Response, error) {
	target := s.baseURL + suffix
	leg := func(proxyAuthorization string) (*http.Response, error) {
		return s.attempt(ctx, method, target, contentType, proxyAuthorization, body, headers)
	}
	var resp *http.Response
	var err error
	if proxyURL := s.plainProxyFor(target); proxyURL != nil {
		resp, err = s.proxyAuthenticate(ctx, proxyURL, nil, leg)
	} else {
		resp, err = leg("")
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusProxyAuthRequired {
		drainClose(resp)
		return nil, ErrProxyAuthRequired
	}
	s.adoptRedirect(resp.Request)
	return resp, nil
}

func (s *httpSession) unauthorizedError(h http.Header) error {
	challenges := parseAuthChallenges(h, headerWWWAuthenticate)
	scheme := cmp.Or(s.attemptedScheme, selectAuthScheme(challenges))
	if scheme == "" {
		if len(challenges) > 0 {
			return ErrAuthSchemeUnsupported
		}
		if s.opts.Credentials == nil {
			return ErrNoCredentials
		}
		return ErrAuthRequired
	}
	if s.opts.Credentials == nil && !s.integratedAttempted {
		return ErrNoCredentials
	}
	switch scheme {
	case schemeNTLM:
		return ErrNTLMAuthFailed
	case schemeNegotiate:
		return ErrNegotiateAuthFailed
	default:
		return ErrAuthRequired
	}
}

func (s *httpSession) doRequest(ctx context.Context, method, suffix, contentType string, body *requestBody, headers map[string]string) (*http.Response, error) {
	resp, err := s.send(ctx, method, suffix, contentType, body, headers)
	if err != nil {
		return nil, err
	}
	s.attemptedScheme = ""
	resp, err = s.authorize(ctx, resp, method, suffix, contentType, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		s.rejectCredential(ctx, &s.supplied)
		return nil, s.unauthorizedError(resp.Header)
	}
	if resp.StatusCode == http.StatusOK {
		s.approveCredential(ctx, s.supplied)
	}
	return s.checkStatus(resp)
}

func (s *httpSession) authorize(ctx context.Context, resp *http.Response, method, suffix, contentType string, body *requestBody, headers map[string]string) (*http.Response, error) {
	if resp.StatusCode != http.StatusUnauthorized || (s.opts.Credentials == nil && s.settings.emptyAuth == emptyAuthOff) {
		return resp, nil
	}
	challenges := parseAuthChallenges(resp.Header, headerWWWAuthenticate)
	scheme := selectAuthScheme(challenges)
	switch {
	case scheme == schemeNTLM || scheme == schemeNegotiate:
		return s.connectionAuthLoop(ctx, resp, method, suffix, contentType, body, headers, scheme)
	case len(challenges) == 0 || scheme == schemeBasic || scheme == schemeBearer:
		return s.basicRetry(ctx, resp, method, suffix, contentType, body, headers)
	default:
		return resp, nil
	}
}

func (s *httpSession) basicRetry(ctx context.Context, resp *http.Response, method, suffix, contentType string, body *requestBody, headers map[string]string) (*http.Response, error) {
	for resp.StatusCode == http.StatusUnauthorized && s.credAttempts < 2 && s.opts.Credentials != nil {
		_ = resp.Body.Close()
		s.rejectCredential(ctx, &s.supplied)
		if err := s.authenticate(ctx); err != nil {
			return nil, err
		}
		next, err := s.send(ctx, method, suffix, contentType, body, headers)
		if err != nil {
			return nil, err
		}
		resp = next
	}
	return resp, nil
}

func (s *httpSession) checkStatus(resp *http.Response) (*http.Response, error) {
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusForbidden:
		_ = resp.Body.Close()
		return nil, ErrAccessDenied
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, ErrRepositoryNotFound
	case http.StatusUnsupportedMediaType:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %w", ErrProtocol, errUnsupportedMediaType)
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: unexpected http status %s", ErrProtocol, resp.Status)
	}
}

func expectServiceHeader(r io.Reader, service Service) error {
	dec := NewDecoder(r)
	line, typ, err := readOnePktLine(dec)
	if err != nil {
		return err
	}
	if typ != PktData || line != "# service="+string(service) {
		return fmt.Errorf("%w: unexpected service header %q", ErrProtocol, line)
	}
	_, typ, err = readOnePktLine(dec)
	if err != nil {
		return err
	}
	if typ != PktFlush {
		return fmt.Errorf("%w: expected a flush after the service header", ErrProtocol)
	}
	return nil
}

func (s *httpSession) Advertise(ctx context.Context) (Advertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Advertisement{}, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	return s.advertiseLocked(ctx)
}

func (s *httpSession) advertiseLocked(ctx context.Context) (Advertisement, error) {
	headers := map[string]string{}
	if s.opts.Version != 1 {
		headers["Git-Protocol"] = "version=2"
	}
	resp, err := s.doRequest(ctx, http.MethodGet, "/info/refs?service="+string(s.service), "", nil, headers)
	if err != nil {
		return Advertisement{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	wantCT := "application/x-" + string(s.service) + "-advertisement"
	if ct := resp.Header.Get("Content-Type"); !hasMediaType(ct, wantCT) {
		return Advertisement{}, fmt.Errorf("%w: unexpected content type %q for the advertisement", ErrProtocol, ct)
	}
	if err := expectServiceHeader(resp.Body, s.service); err != nil {
		return Advertisement{}, err
	}
	adv, err := ParseAdvertisement(resp.Body)
	if err != nil {
		return Advertisement{}, err
	}
	s.version = adv.Version
	if s.version == 0 {
		s.version = 1
	}
	s.caps = adv.Capabilities
	if s.version == 2 {
		refs, head, err := lsRefsV2(ctx, httpRoundTripper{session: s}, agentValue(s.opts))
		if err != nil {
			return Advertisement{}, err
		}
		adv.Refs = refs
		adv.Head = head
	}
	adv.Version = s.version
	s.advertised = true
	return adv, nil
}

func (s *httpSession) adoptRedirect(final *http.Request) {
	if final == nil {
		return
	}
	target := url.URL{Scheme: final.URL.Scheme, Host: final.URL.Host, Path: final.URL.Path, RawPath: final.URL.RawPath}
	base, ok := strings.CutSuffix(target.String(), "/info/refs")
	if !ok || base == s.baseURL {
		return
	}
	if !strings.HasPrefix(s.baseURL+"/", target.Scheme+"://"+target.Host+"/") {
		s.resource = CredentialResource(Endpoint{
			Scheme: Scheme(target.Scheme),
			Host:   target.Hostname(),
			Port:   target.Port(),
			Path:   strings.TrimSuffix(target.Path, "/info/refs"),
		})
		s.endpoint.User = ""
		s.password.Wipe()
		s.authHeader = ""
		forgetCredential(&s.supplied)
		s.credAttempts = 0
	}
	s.baseURL = base
}

func (s *httpSession) ensureAdvertised(ctx context.Context) error {
	if s.advertised {
		return nil
	}
	_, err := s.advertiseLocked(ctx)
	return err
}

type httpRoundTripper struct {
	session *httpSession
}

func (s *httpSession) compressesRequest(body []byte) bool {
	return s.service == UploadPack && s.version != 2 && !s.plainRequests && len(body) > gzipRequestThreshold
}

func (rt httpRoundTripper) round(ctx context.Context, body []byte) (io.ReadCloser, error) {
	s := rt.session
	headers := map[string]string{}
	if s.version == 2 {
		headers["Git-Protocol"] = "version=2"
	}
	if s.compressesRequest(body) {
		compressed := maps.Clone(headers)
		compressed["Content-Encoding"] = "gzip"
		result, err := s.post(ctx, bytesBody(gzipBytes(body)), compressed)
		if !errors.Is(err, errUnsupportedMediaType) {
			return result, err
		}
		s.plainRequests = true
	}
	return s.post(ctx, bytesBody(body), headers)
}

func (s *httpSession) post(ctx context.Context, body *requestBody, headers map[string]string) (io.ReadCloser, error) {
	reqCT := "application/x-" + string(s.service) + "-request"
	resp, err := s.doRequest(ctx, http.MethodPost, "/"+string(s.service), reqCT, body, headers)
	if err != nil {
		return nil, err
	}
	wantCT := "application/x-" + string(s.service) + "-result"
	if ct := resp.Header.Get("Content-Type"); !hasMediaType(ct, wantCT) {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: unexpected content type %q for %s", ErrProtocol, ct, s.service)
	}
	return resp.Body, nil
}

func (s *httpSession) Fetch(ctx context.Context, req FetchRequest, neg Negotiator) (*FetchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.ensureAdvertised(ctx); err != nil {
		return nil, err
	}
	rt := httpRoundTripper{session: s}
	agent := agentValue(s.opts)
	if s.version == 2 {
		return fetchV2(ctx, rt, req, neg, s.caps, agent)
	}
	return fetchV1(ctx, rt, req, neg, s.caps, agent, true)
}

func (s *httpSession) Push(ctx context.Context, req PushRequest) (*PushResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.ensureAdvertised(ctx); err != nil {
		return nil, err
	}
	prefix, _ := buildPushRequest(req, s.caps, agentValue(s.opts))
	body, cleanup, err := pushBody(prefix, req.Pack, s.settings.postBuffer, s.opts.Credentials != nil)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	resp, err := s.doRequest(ctx, http.MethodPost, "/"+string(ReceivePack), "application/x-"+string(ReceivePack)+"-request", body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	wantCT := "application/x-" + string(ReceivePack) + "-result"
	if ct := resp.Header.Get("Content-Type"); !hasMediaType(ct, wantCT) {
		return nil, fmt.Errorf("%w: unexpected content type %q for the push result", ErrProtocol, ct)
	}
	return finishPush(resp.Body, s.caps)
}
