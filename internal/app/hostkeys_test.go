package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"golang.org/x/crypto/ssh"

	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/ui/hostkey"
)

func stubHostKeyDialog(t *testing.T, a *App, respond func(req hostkey.Request) bool) {
	t.Helper()
	prev := newHostKeyView
	newHostKeyView = func(eng widget.ModalShower, req hostkey.Request) (*hostkey.View, error) {
		view, err := prev(eng, req)
		if err != nil {
			return nil, err
		}
		accept := respond(req)
		a.Post(func() {
			if accept {
				view.OnAccept()
			} else {
				view.OnReject()
			}
		})
		return view, nil
	}
	t.Cleanup(func() { newHostKeyView = prev })
}

func TestConfirmHostKeyAcceptShowsTheOfferedFingerprint(t *testing.T) {
	a := newTestApp(t)
	var seen hostkey.Request
	stubHostKeyDialog(t, a, func(req hostkey.Request) bool {
		seen = req
		return true
	})

	key := transport.HostKey{Host: "example.com", Algorithm: "ssh-ed25519", Fingerprint: "SHA256:abc"}
	ok, err := a.confirmHostKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("accepted host key must report ok")
	}
	if seen.Host != key.Host || seen.Algorithm != key.Algorithm || seen.Fingerprint != key.Fingerprint {
		t.Fatalf("dialog request = %+v, want %+v", seen, key)
	}
}

func TestConfirmHostKeyReject(t *testing.T) {
	a := newTestApp(t)
	stubHostKeyDialog(t, a, func(hostkey.Request) bool { return false })

	ok, err := a.confirmHostKey(context.Background(), transport.HostKey{Host: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("rejected host key must report not ok")
	}
}

func TestConfirmHostKeyContextCancelled(t *testing.T) {
	a := newTestApp(t)
	prev := newHostKeyView
	opened := make(chan struct{})
	newHostKeyView = func(eng widget.ModalShower, req hostkey.Request) (*hostkey.View, error) {
		view, err := prev(eng, req)
		close(opened)
		return view, err
	}
	t.Cleanup(func() { newHostKeyView = prev })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.confirmHostKey(ctx, transport.HostKey{Host: "example.com"})
		done <- err
	}()
	<-opened
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestConfirmHostKeyDialogConstructionFailureRejects(t *testing.T) {
	a := newTestApp(t)
	prev := newHostKeyView
	newHostKeyView = func(widget.ModalShower, hostkey.Request) (*hostkey.View, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { newHostKeyView = prev })

	ok, err := a.confirmHostKey(context.Background(), transport.HostKey{Host: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("a failed dialog must not accept the key")
	}
}

func TestHostKeyPolicyAcceptsAKnownKeyWithoutADialog(t *testing.T) {
	a := newTestApp(t)
	prev := newHostKeyView
	newHostKeyView = func(widget.ModalShower, hostkey.Request) (*hostkey.View, error) {
		t.Fatal("host key dialog must not open for an already known key")
		return nil, nil
	}
	t.Cleanup(func() { newHostKeyView = prev })

	policy := a.hostKeyPolicy()
	key := transport.HostKey{Host: "example.com:22", Algorithm: "ssh-ed25519", Key: testEd25519PublicKey(t)}
	if err := policy.Accept(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check(context.Background(), key); err != nil {
		t.Fatalf("a previously accepted key must be trusted silently: %v", err)
	}
	if _, err := os.Stat(a.paths.KnownHosts()); err != nil {
		t.Fatalf("known hosts file was not written: %v", err)
	}
}

func testEd25519PublicKey(t *testing.T) []byte {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return sshPub.Marshal()
}
