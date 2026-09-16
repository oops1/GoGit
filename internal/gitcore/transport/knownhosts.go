package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrHostKeyChanged  = errors.New("transport: host key has changed")
	ErrHostKeyRejected = errors.New("transport: host key was rejected")
)

var (
	newKnownHostsCallback = knownhosts.New
	makeKnownHostsScratch = func() (string, error) { return os.MkdirTemp("", "gogit-known-hosts-") }
)

type stringAddr string

func (stringAddr) Network() string { return "tcp" }

func (a stringAddr) String() string { return string(a) }

type knownHostsPolicy struct {
	mu        *sync.Mutex
	path      string
	confirm   func(context.Context, HostKey) (bool, error)
	userFiles []string
	strict    string
}

func NewKnownHosts(path string, confirm func(context.Context, HostKey) (bool, error)) HostKeyPolicy {
	return &knownHostsPolicy{mu: &sync.Mutex{}, path: path, confirm: confirm}
}

func (p *knownHostsPolicy) forHop(userFiles []string, strict string) *knownHostsPolicy {
	next := *p
	if len(userFiles) > 0 {
		next.userFiles = userFiles
	}
	if strict != "" {
		next.strict = strict
	}
	return &next
}

func homeKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func existingFiles(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (p *knownHostsPolicy) buildCallback() (ssh.HostKeyCallback, error) {
	userFiles := p.userFiles
	if len(userFiles) == 0 {
		userFiles = []string{homeKnownHostsPath()}
	}
	return knownHostsCallback(existingFiles(append([]string{p.path}, userFiles...)...))
}

func knownHostsCallback(files []string) (ssh.HostKeyCallback, error) {
	readable := make([]string, 0, len(files))
	scratch := ""
	defer func() {
		if scratch != "" {
			_ = os.RemoveAll(scratch)
		}
	}()
	for i, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		kept, dropped := filterKnownHosts(data)
		if !dropped {
			readable = append(readable, file)
			continue
		}
		if scratch == "" {
			if scratch, err = makeKnownHostsScratch(); err != nil {
				return nil, err
			}
		}
		copyPath := filepath.Join(scratch, strconv.Itoa(i))
		if err := os.WriteFile(copyPath, kept, 0o600); err != nil {
			return nil, err
		}
		readable = append(readable, copyPath)
	}
	return newKnownHostsCallback(readable...)
}

func filterKnownHosts(data []byte) ([]byte, bool) {
	var kept bytes.Buffer
	dropped := false
	for line := range bytes.Lines(data) {
		if knownHostsLineUsable(line) {
			kept.Write(line)
			continue
		}
		dropped = true
	}
	return kept.Bytes(), dropped
}

func knownHostsLineUsable(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] == '#' {
		return true
	}
	marker, _, _, _, _, err := ssh.ParseKnownHosts(trimmed)
	if err != nil {
		return false
	}
	fields := bytes.Fields(trimmed)
	if marker != "" {
		if marker != "cert-authority" && marker != "revoked" {
			return false
		}
		fields = fields[1:]
	}
	return knownHostsPatternUsable(string(fields[0]))
}

func knownHostsPatternUsable(pattern string) bool {
	if strings.HasPrefix(pattern, "|") {
		parts := strings.Split(pattern, "|")
		if len(parts) != 4 || parts[1] != "1" {
			return false
		}
		_, saltErr := base64.StdEncoding.DecodeString(parts[2])
		_, hashErr := base64.StdEncoding.DecodeString(parts[3])
		return saltErr == nil && hashErr == nil
	}
	for part := range strings.SplitSeq(pattern, ",") {
		negated := strings.TrimPrefix(part, "!")
		if part != "" && negated == "" {
			return false
		}
		if strings.HasPrefix(negated, "[") {
			if _, _, err := net.SplitHostPort(negated); err != nil {
				return false
			}
		}
	}
	return true
}

func (p *knownHostsPolicy) Check(ctx context.Context, key HostKey) error {
	pub, err := ssh.ParsePublicKey(key.Key)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHostKeyRejected, err)
	}
	p.mu.Lock()
	cb, err := p.buildCallback()
	p.mu.Unlock()
	if err != nil {
		return err
	}
	err = cb(key.Host, stringAddr(key.Host), pub)
	if err == nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("%w: %s", ErrHostKeyChanged, key.Host)
		}
		return p.unknownHostKey(ctx, key)
	}
	return err
}

func (p *knownHostsPolicy) unknownHostKey(ctx context.Context, key HostKey) error {
	switch p.strict {
	case "yes", "true":
		return fmt.Errorf("%w: unknown host key for %s and StrictHostKeyChecking is on", ErrHostKeyRejected, key.Host)
	case "no", "off", "false", "accept-new":
		return p.Accept(ctx, key)
	default:
		return p.confirmAndAccept(ctx, key)
	}
}

func (p *knownHostsPolicy) HostKeyAlgorithms(host string) []string {
	p.mu.Lock()
	cb, err := p.buildCallback()
	p.mu.Unlock()
	if err != nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(cb(host, stringAddr(host), probeHostKey), &keyErr) {
		return nil
	}
	var algorithms []string
	for _, known := range keyErr.Want {
		for _, algorithm := range hostKeyAlgorithmsFor(known.Key.Type()) {
			if !slices.Contains(algorithms, algorithm) {
				algorithms = append(algorithms, algorithm)
			}
		}
	}
	return algorithms
}

func hostKeyAlgorithmsFor(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

func (p *knownHostsPolicy) confirmAndAccept(ctx context.Context, key HostKey) error {
	if p.confirm == nil {
		return fmt.Errorf("%w: unknown host key for %s", ErrHostKeyRejected, key.Host)
	}
	ok, err := p.confirm(ctx, key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: unknown host key for %s", ErrHostKeyRejected, key.Host)
	}
	return p.Accept(ctx, key)
}

func (p *knownHostsPolicy) Accept(_ context.Context, key HostKey) error {
	pub, err := ssh.ParsePublicKey(key.Key)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHostKeyRejected, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if dir := filepath.Dir(p.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer closeQuietly(f)
	_, err = f.WriteString(knownhosts.Line([]string{key.Host}, pub) + "\n")
	return err
}
