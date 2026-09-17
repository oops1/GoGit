//go:build !windows

package transport

func newIntegratedGenerator(_ string, _ string) (authGenerator, bool, error) {
	return nil, false, nil
}
