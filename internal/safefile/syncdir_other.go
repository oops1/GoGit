//go:build !linux

package safefile

func syncDir(string) error {
	return nil
}
