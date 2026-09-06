package transport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrHostKeyChanged  = errors.New("transport: host key has changed")
	ErrHostKeyRejected = errors.New("transport: host key was rejected")
)

type stringAddr string

func (stringAddr) Network() string { return "tcp" }

func (a stringAddr) String() string { return string(a) }

type knownHostsPolicy struct {
	mu      sync.Mutex
	path    string
	confirm func(context.Context, HostKey) (bool, error)
}

func NewKnownHosts(path string, confirm func(context.Context, HostKey) (bool, error)) HostKeyPolicy {
	return &knownHostsPolicy{path: path, confirm: confirm}
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
	files := existingFiles(p.path, homeKnownHostsPath())
	return knownhosts.New(files...)
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
		return p.confirmAndAccept(ctx, key)
	}
	return err
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
