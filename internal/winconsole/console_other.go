//go:build !windows

package winconsole

func Hide() bool { return false }
