package transport

import (
	"bufio"
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

var errProxyConnectRefused = errors.New("transport: the proxy refused to open a tunnel")

func (s *httpSession) tlsConfigFor(host string) *tls.Config {
	cfg := s.tlsConfig.Clone()
	if cfg.ServerName == "" {
		cfg.ServerName = host
	}
	return cfg
}

func (s *httpSession) handshakeTLS(ctx context.Context, conn net.Conn, host string) (*tls.Conn, error) {
	tc := tls.Client(conn, s.tlsConfigFor(host))
	hsCtx, cancel := context.WithTimeout(ctx, tlsHandshakeTimeout)
	defer cancel()
	if err := tc.HandshakeContext(hsCtx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tc, nil
}

func (s *httpSession) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	proxyURL, err := s.proxy.tunnelFor(addr)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	if proxyURL != nil {
		conn, err = s.dialTunnel(ctx, proxyURL, addr)
	} else {
		conn, err = s.dialer.DialContext(ctx, network, addr)
	}
	if err != nil {
		return nil, err
	}
	return s.handshakeTLS(ctx, conn, host)
}

type proxyTunnel struct {
	session  *httpSession
	proxyURL *url.URL
	target   string
	conn     net.Conn
	reader   *bufio.Reader
	binding  []byte
	reopen   bool
}

func (t *proxyTunnel) open(ctx context.Context) error {
	if t.conn != nil {
		_ = t.conn.Close()
	}
	conn, err := t.session.dialer.DialContext(ctx, "tcp", t.proxyURL.Host)
	if err != nil {
		return err
	}
	if t.proxyURL.Scheme == string(SchemeHTTPS) {
		tc, err := t.session.handshakeTLS(ctx, conn, t.proxyURL.Hostname())
		if err != nil {
			return err
		}
		state := tc.ConnectionState()
		t.binding = channelBindingFromState(&state)
		conn = tc
	}
	t.conn, t.reader, t.reopen = conn, bufio.NewReader(conn), false
	return nil
}

func (t *proxyTunnel) leg(ctx context.Context) func(string) (*http.Response, error) {
	return func(authorization string) (*http.Response, error) {
		if t.reopen {
			if err := t.open(ctx); err != nil {
				return nil, err
			}
		}
		req := &http.Request{
			Method: http.MethodConnect,
			URL:    &url.URL{Opaque: t.target},
			Host:   t.target,
			Header: http.Header{},
		}
		req.Header.Set("User-Agent", cmp.Or(t.session.opts.UserAgent, defaultUserAgent))
		if authorization != "" {
			req.Header.Set("Proxy-Authorization", authorization)
		}
		conn := t.conn
		stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
		defer stop()
		_ = conn.SetDeadline(time.Now().Add(discoveryHeaderTimeout))
		if err := req.Write(conn); err != nil {
			return nil, err
		}
		resp, err := http.ReadResponse(t.reader, req)
		if err != nil {
			return nil, err
		}
		t.reopen = resp.Close
		return resp, nil
	}
}

func (s *httpSession) dialTunnel(ctx context.Context, proxyURL *url.URL, target string) (net.Conn, error) {
	t := &proxyTunnel{session: s, proxyURL: proxyURL, target: target}
	if err := t.open(ctx); err != nil {
		return nil, err
	}
	resp, err := s.proxyAuthenticate(ctx, proxyURL, t.binding, t.leg(ctx))
	if err != nil {
		_ = t.conn.Close()
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		_ = t.conn.Close()
		return nil, fmt.Errorf("%w: %s answered %s", errProxyConnectRefused, proxyResource(proxyURL), resp.Status)
	}
	_ = t.conn.SetDeadline(time.Time{})
	return &tunnelConn{Conn: t.conn, reader: t.reader}, nil
}

type tunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *tunnelConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
