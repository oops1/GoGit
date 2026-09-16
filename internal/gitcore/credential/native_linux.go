package credential

const (
	gcmDefaultStore  = ""
	gcmFileNewline   = "\n"
	gcmPortSeparator = ":"
)

var (
	windowsCredentials credentialManager
	openKeyring        = openDBusKeyring
)
