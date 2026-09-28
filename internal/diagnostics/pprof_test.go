package diagnostics

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTheProfilerStaysOffWithoutTheVariable(t *testing.T) {
	t.Setenv(EnvProfileAddress, "")
	called := false
	prev := listen
	listen = func(string, string) (net.Listener, error) {
		called = true
		return nil, errors.New("must not listen")
	}
	t.Cleanup(func() { listen = prev })

	StartProfiler(quietLogger())

	if called {
		t.Fatal("the profiler opened a port although the variable is empty")
	}
}

func TestTheProfilerRefusesAnAddressOutsideTheLoopback(t *testing.T) {
	for _, address := range []string{"0.0.0.0:6060", "192.168.3.5:6060", "[::]:6060"} {
		if _, err := listenLoopback(address); !errors.Is(err, ErrRemoteAddress) {
			t.Fatalf("%s gave %v, want %v", address, err, ErrRemoteAddress)
		}
	}
}

func TestLocalhostCountsAsTheLoopback(t *testing.T) {
	for host, want := range map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true, "example.com": false, "": false} {
		if got := isLoopback(host); got != want {
			t.Fatalf("isLoopback(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestTheProfilerRefusesAnAddressItCannotRead(t *testing.T) {
	if _, err := listenLoopback("not-an-address"); err == nil {
		t.Fatal("an address without a port was accepted")
	}
}

func TestTheProfilerServesHeapOverTheLoopback(t *testing.T) {
	t.Setenv(EnvProfileAddress, "127.0.0.1:0")
	var opened net.Listener
	prev := listen
	listen = func(network, address string) (net.Listener, error) {
		listener, err := prev(network, address)
		opened = listener
		return listener, err
	}
	t.Cleanup(func() { listen = prev })

	StartProfiler(quietLogger())
	if opened == nil {
		t.Fatal("the profiler did not open a port")
	}
	t.Cleanup(func() { _ = opened.Close() })

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + opened.Addr().String() + "/debug/pprof/heap?debug=1")
	if err != nil {
		t.Fatalf("get returned error %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read returned error %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "heap profile") {
		t.Fatalf("status = %d, body starts with %q", resp.StatusCode, string(body[:min(len(body), 60)]))
	}
}

func TestTheProfilerLogsWhenThePortIsBusy(t *testing.T) {
	t.Setenv(EnvProfileAddress, "127.0.0.1:6060")
	prev := listen
	listen = func(string, string) (net.Listener, error) { return nil, errors.New("port is busy") }
	t.Cleanup(func() { listen = prev })

	StartProfiler(quietLogger())
}
