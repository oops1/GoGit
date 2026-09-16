//go:build !windows && !linux

package credential

const (
	gcmDefaultStore  = ""
	gcmFileNewline   = "\n"
	gcmPortSeparator = ":"
)

var (
	windowsCredentials credentialManager
	openKeyring        func() (keyring, error)
)
