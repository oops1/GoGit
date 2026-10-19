package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"
)

func swapTLSHandshakeTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	original := tlsHandshakeTimeout
	tlsHandshakeTimeout = d
	t.Cleanup(func() { tlsHandshakeTimeout = original })
}

func silentListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	return ln
}

func TestHTTPDiscoveryFailsWithHandshakeTimeoutWhenTheServerNeverAnswersTLS(t *testing.T) {
	clearProxyEnvironment(t)
	swapTLSHandshakeTimeout(t, 50*time.Millisecond)
	ln := silentListener(t)
	err := advertiseThrough(t, "https://"+ln.Addr().String()+"/repo.git", Options{})
	if !errors.Is(err, ErrTLSHandshakeTimeout) {
		t.Fatalf("advertise returned %v, want ErrTLSHandshakeTimeout", err)
	}
}

func TestHandshakeReportsCancellationInsteadOfTimeout(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)
	s := &httpSession{tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	_, err := s.handshakeTLS(ctx, client, "example.com")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrTLSHandshakeTimeout) {
		t.Fatalf("handshakeTLS returned %v, want context.Canceled", err)
	}
}

type slowHandshakeListener struct {
	net.Listener
	delay time.Duration
}

func (l slowHandshakeListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &slowFirstReadConn{Conn: conn, delay: l.delay}, nil
}

type slowFirstReadConn struct {
	net.Conn
	delay time.Duration
	read  bool
}

func (c *slowFirstReadConn) Read(p []byte) (int, error) {
	if !c.read {
		c.read = true
		time.Sleep(c.delay)
	}
	return c.Conn.Read(p)
}

func TestHTTPDiscoveryHeaderTimeoutDoesNotCountTheTLSHandshake(t *testing.T) {
	clearProxyEnvironment(t)
	swapDiscoveryHeaderTimeout(t, 100*time.Millisecond)
	server := httptest.NewUnstartedServer(http.HandlerFunc(advertiseV1Handler))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Listener = slowHandshakeListener{Listener: server.Listener, delay: 300 * time.Millisecond}
	server.StartTLS()
	t.Cleanup(server.Close)
	cfg := testGitConfig(t, "[http]\n\tsslCAInfo = "+writeServerCertificate(t, server)+"\n")
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg}); err != nil {
		t.Fatalf("advertise returned %v after a slow handshake", err)
	}
}

func TestHeaderDeadlineIgnoresStartAfterStop(t *testing.T) {
	d := &headerDeadline{cancel: func(error) { t.Errorf("cancel fired after stop") }}
	d.stop()
	d.start(httptrace.WroteRequestInfo{})
	if d.timer != nil {
		t.Fatalf("start armed a timer after stop")
	}
}

func TestHeaderDeadlineKeepsTheFirstTimerOnRepeatedStart(t *testing.T) {
	d := &headerDeadline{cancel: func(error) {}}
	d.start(httptrace.WroteRequestInfo{})
	first := d.timer
	d.start(httptrace.WroteRequestInfo{})
	d.stop()
	if d.timer != first {
		t.Fatalf("a repeated start replaced the running timer")
	}
}
