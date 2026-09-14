package transport

import (
	"crypto/ed25519"

	"golang.org/x/crypto/ssh"
)

var probeHostKey, _ = ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
