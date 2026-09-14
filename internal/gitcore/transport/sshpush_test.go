package transport

import (
	"bytes"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestSSHPushLockedPropagatesTheRequestWriteError(t *testing.T) {
	wantErr := errors.New("channel closed")
	session := &sshSession{advertised: true, stdin: erroringWriteCloser{err: wantErr}}
	_, err := session.pushLocked(t.Context(), PushRequest{
		Updates: []Update{{Name: "refs/heads/main", Old: hash.Zero, New: idOf(1)}},
		Pack:    bytes.NewReader(nil),
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("pushLocked returned %v, want %v", err, wantErr)
	}
}
