package attributes

import (
	"encoding/binary"
	"strings"
)

var iconvUnmarked = unmarkedUTF{order: binary.BigEndian, bom16: utf16BEBOM, bom32: utf32BEBOM}

func iconvUTFName(name string) string {
	return strings.ToUpper(name)
}
