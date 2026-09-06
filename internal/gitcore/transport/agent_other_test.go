//go:build !windows

package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh/agent"
)

func TestDialAgentFailsWhenSSHAuthSockIsUnset(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := dialAgent()
	if err == nil {
		t.Fatalf("dialAgent succeeded, want an error")
	}
}

func TestDialAgentConnectsThroughSSHAuthSock(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen returned error %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey returned error %v", err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("Add returned error %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = agent.ServeAgent(keyring, conn)
	}()

	t.Setenv("SSH_AUTH_SOCK", sockPath)
	conn, err := dialAgent()
	if err != nil {
		t.Fatalf("dialAgent returned error %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := agent.NewClient(conn)
	signers, err := client.Signers()
	if err != nil {
		t.Fatalf("Signers returned error %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("len(signers) = %d, want 1", len(signers))
	}
}

func TestDialAgentFailsForUnreachableSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))
	_, err := dialAgent()
	if err == nil {
		t.Fatalf("dialAgent succeeded, want an error")
	}
}
