//go:build !windows

package app

import "testing"

func TestSystemInterfaceFontIsNoneOutsideWindows(t *testing.T) {
	if got := platformInterfaceFont(); got != (interfaceFontFiles{}) {
		t.Errorf("platformInterfaceFont = %+v, want none", got)
	}
}
