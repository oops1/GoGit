//go:build !windows

package transport

func newIntegratedGenerator(scheme, host string, _ []byte) (authGenerator, bool) {
	if scheme != schemeNegotiate || host == "" {
		return nil, false
	}
	return newKerberosGenerator(host)
}
