package app

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	systemInterfaceFont = func() interfaceFontFiles { return interfaceFontFiles{} }
	os.Exit(m.Run())
}
