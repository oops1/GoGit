package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const defaultGitPort = "9418"

func buildGitRequestLine(service Service, path, host string, wantV2 bool) []byte {
	text := string(service) + " " + path + "\x00host=" + host + "\x00"
	if wantV2 {
		text += "\x00version=2\x00"
	}
	return appendPktLine(nil, text)
}

func watchContext(ctx context.Context, conn net.Conn) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-done:
		}
	}()
	return func() { close(done) }
}

type noCloseReader struct {
	io.Reader
}

func (noCloseReader) Close() error { return nil }

type gitSession struct {
	mu         sync.Mutex
	endpoint   Endpoint
	service    Service
	opts       Options
	conn       net.Conn
	advertised bool
	version    int
	caps       Capabilities
	closed     bool
}

func newGitSession(endpoint Endpoint, service Service, opts Options) *gitSession {
	return &gitSession{endpoint: endpoint, service: service, opts: opts}
}

func (s *gitSession) connect(ctx context.Context) error {
	if s.conn != nil {
		return nil
	}
	port := s.endpoint.Port
	if port == "" {
		port = defaultGitPort
	}
	addr := net.JoinHostPort(s.endpoint.Host, port)
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	stop := watchContext(ctx, conn)
	request := buildGitRequestLine(s.service, s.endpoint.Path, s.endpoint.Host, s.opts.Version != 1)
	_, writeErr := conn.Write(request)
	stop()
	if writeErr != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return writeErr
	}
	s.conn = conn
	return nil
}

func (s *gitSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

type gitRoundTripper struct {
	session *gitSession
}

func (rt gitRoundTripper) round(ctx context.Context, body []byte) (io.ReadCloser, error) {
	s := rt.session
	if _, err := s.conn.Write(body); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return noCloseReader{Reader: s.conn}, nil
}

func (s *gitSession) advertiseLocked(ctx context.Context) (Advertisement, error) {
	adv, err := ParseAdvertisement(s.conn)
	if err != nil {
		return Advertisement{}, err
	}
	s.version = adv.Version
	if s.version == 0 {
		s.version = 1
	}
	s.caps = adv.Capabilities
	if s.version == 2 {
		refs, head, err := lsRefsV2(ctx, gitRoundTripper{session: s}, agentValue(s.opts))
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

func (s *gitSession) ensureAdvertised(ctx context.Context) error {
	if s.advertised {
		return nil
	}
	_, err := s.advertiseLocked(ctx)
	return err
}

func (s *gitSession) Advertise(ctx context.Context) (Advertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Advertisement{}, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.connect(ctx); err != nil {
		return Advertisement{}, err
	}
	stop := watchContext(ctx, s.conn)
	defer stop()
	adv, err := s.advertiseLocked(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return Advertisement{}, ctx.Err()
		}
		return Advertisement{}, err
	}
	return adv, nil
}

func (s *gitSession) Fetch(ctx context.Context, req FetchRequest, neg Negotiator) (*FetchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.connect(ctx); err != nil {
		return nil, err
	}
	stop := watchContext(ctx, s.conn)
	release := true
	defer func() {
		if release {
			stop()
		}
	}()
	if err := s.ensureAdvertised(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	rt := gitRoundTripper{session: s}
	agent := agentValue(s.opts)
	var resp *FetchResponse
	var err error
	if s.version == 2 {
		resp, err = fetchV2(ctx, rt, req, neg, s.caps, agent)
	} else {
		resp, err = fetchV1(ctx, rt, req, neg, s.caps, agent, false)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if resp.Pack != nil {
		release = false
		pack := resp.Pack
		resp.Pack = readCloser{Reader: pack, closer: func() error {
			stop()
			return pack.Close()
		}}
	}
	return resp, nil
}

func (s *gitSession) Push(ctx context.Context, req PushRequest) (*PushResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: session is closed", ErrProtocol)
	}
	if err := s.connect(ctx); err != nil {
		return nil, err
	}
	stop := watchContext(ctx, s.conn)
	defer stop()
	result, err := s.pushLocked(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return result, nil
}

func (s *gitSession) pushLocked(ctx context.Context, req PushRequest) (*PushResult, error) {
	if err := s.ensureAdvertised(ctx); err != nil {
		return nil, err
	}
	prefix, _ := buildPushRequest(req, s.caps, agentValue(s.opts))
	if _, err := s.conn.Write(prefix); err != nil {
		return nil, err
	}
	if _, err := io.Copy(s.conn, req.Pack); err != nil {
		return nil, err
	}
	return finishPush(s.conn, s.caps)
}
