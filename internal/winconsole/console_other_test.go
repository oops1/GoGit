//go:build !windows

package winconsole

import "testing"

func TestHideDoesNothingOutsideWindows(t *testing.T) {
	if Hide() {
		t.Fatal("there is no console to hide outside windows")
	}
}
