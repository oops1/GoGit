package diagnostics

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"
)

const (
	EnvProfileAddress = "GOGIT_PPROF"

	readHeaderTimeout = 5 * time.Second
)

var ErrRemoteAddress = errors.New("diagnostics: the profiler listens on the loopback interface only")

var listen = net.Listen

func StartProfiler(log *slog.Logger) {
	address := strings.TrimSpace(os.Getenv(EnvProfileAddress))
	if address == "" {
		return
	}
	listener, err := listenLoopback(address)
	if err != nil {
		log.Warn("profiler did not start", "address", address, "error", err)
		return
	}
	log.Warn("profiler listens", "address", listener.Addr().String())
	go serve(listener, log)
}

func listenLoopback(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if !isLoopback(host) {
		return nil, ErrRemoteAddress
	}
	return listen("tcp", address)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func serve(listener net.Listener, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("profiler stopped", "error", err)
	}
}
