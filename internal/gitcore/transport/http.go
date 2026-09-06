package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultUserAgent = "git/2.0 (gogit)"

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 5 * time.Second,
		},
	}
}

func httpBaseURL(e Endpoint) string {
	host := e.Host
	if e.Port != "" {
		host = net.JoinHostPort(e.Host, e.Port)
	}
	return string(e.Scheme) + "://" + host + strings.TrimSuffix(e.Path, "/")
}

type httpSession struct {
	mu           sync.Mutex
	endpoint     Endpoint
	password     Password
	service      Service
	opts         Options
	client       *http.Client
	baseURL      string
	resource     string
	advertised   bool
	version      int
	caps         Capabilities
	authHeader   string
	credAttempts int
	closed       bool
}

func newHTTPSession(endpoint Endpoint, password Password, service Service, opts Options) *httpSession {
	client := opts.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}
	return &httpSession{
		endpoint: endpoint,
		password: password,
		service:  service,
		opts:     opts,
		client:   client,
		baseURL:  httpBaseURL(endpoint),
		resource: resourceOf(endpoint),
	}
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
	return nil
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
	defer creds.Wipe()
	if len(creds.Token) > 0 {
		s.authHeader = "Bearer " + string(creds.Token)
		return nil
	}
	s.authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(creds.Username+":"+string(creds.Password)))
	return nil
}

func (s *httpSession) attempt(ctx context.Context, method, url, contentType string, bodyFactory func() io.Reader, headers map[string]string) (*http.Response, error) {
	var body io.Reader
	if bodyFactory != nil {
		body = bodyFactory()
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if s.opts.UserAgent != "" {
		req.Header.Set("User-Agent", s.opts.UserAgent)
	} else {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	s.applyAuthHeader(req)
	return s.client.Do(req)
}

func (s *httpSession) doRequest(ctx context.Context, method, url, contentType string, bodyFactory func() io.Reader, headers map[string]string) (*http.Response, error) {
	resp, err := s.attempt(ctx, method, url, contentType, bodyFactory, headers)
	if err != nil {
		return nil, err
	}
	for resp.StatusCode == http.StatusUnauthorized && s.credAttempts < 2 && s.opts.Credentials != nil {
		_ = resp.Body.Close()
		if err := s.authenticate(ctx); err != nil {
			return nil, err
		}
		resp, err = s.attempt(ctx, method, url, contentType, bodyFactory, headers)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if s.opts.Credentials == nil {
			return nil, ErrNoCredentials
		}
		return nil, ErrAuthRequired
	}
	return s.checkStatus(resp)
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
	url := s.baseURL + "/info/refs?service=" + string(s.service)
	resp, err := s.doRequest(ctx, http.MethodGet, url, "", nil, headers)
	if err != nil {
		return Advertisement{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	wantCT := "application/x-" + string(s.service) + "-advertisement"
	if ct := resp.Header.Get("Content-Type"); ct != wantCT {
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

func (rt httpRoundTripper) round(ctx context.Context, body []byte) (io.ReadCloser, error) {
	s := rt.session
	url := s.baseURL + "/" + string(s.service)
	reqCT := "application/x-" + string(s.service) + "-request"
	headers := map[string]string{}
	if s.version == 2 {
		headers["Git-Protocol"] = "version=2"
	}
	resp, err := s.doRequest(ctx, http.MethodPost, url, reqCT, func() io.Reader { return bytes.NewReader(body) }, headers)
	if err != nil {
		return nil, err
	}
	wantCT := "application/x-" + string(s.service) + "-result"
	if ct := resp.Header.Get("Content-Type"); ct != wantCT {
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
	packBytes, err := io.ReadAll(req.Pack)
	if err != nil {
		return nil, fmt.Errorf("%w: reading the push pack: %w", ErrProtocol, err)
	}
	prefix, _ := buildPushRequest(req, s.caps, agentValue(s.opts))
	url := s.baseURL + "/" + string(ReceivePack)
	reqCT := "application/x-" + string(ReceivePack) + "-request"
	bodyFactory := func() io.Reader {
		return io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(packBytes))
	}
	resp, err := s.doRequest(ctx, http.MethodPost, url, reqCT, bodyFactory, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	wantCT := "application/x-" + string(ReceivePack) + "-result"
	if ct := resp.Header.Get("Content-Type"); ct != wantCT {
		return nil, fmt.Errorf("%w: unexpected content type %q for the push result", ErrProtocol, ct)
	}
	return finishPush(resp.Body, s.caps)
}
