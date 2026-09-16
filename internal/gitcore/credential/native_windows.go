package credential

const (
	gcmDefaultStore  = gcmStoreWincredman
	gcmFileNewline   = "\r\n"
	gcmPortSeparator = "-"
)

var (
	windowsCredentials credentialManager = newAdvapiCredentials()
	openKeyring        func() (keyring, error)
)
