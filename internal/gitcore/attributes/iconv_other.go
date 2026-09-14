//go:build !windows

package attributes

import (
	"encoding/binary"
	"strings"
)

var iconvUnmarked = unmarkedUTF{order: binary.LittleEndian, bom16: utf16LEBOM, bom32: utf32LEBOM}

func iconvUTFName(name string) string {
	dashed := strings.Replace(strings.ToUpper(name), "UTF", "UTF-", 1)
	return strings.Replace(dashed, "UTF--", "UTF-", 1)
}
