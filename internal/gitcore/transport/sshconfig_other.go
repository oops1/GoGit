//go:build !windows

package transport

func systemSSHConfigPath() string {
	return "/etc/ssh/ssh_config"
}
