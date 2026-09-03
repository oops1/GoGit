package repo

import (
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

const defaultFileMode = true

func platformInitValues() []configValue {
	return nil
}

func samePath(a, b string) bool {
	return a == b
}

func deviceID(info fs.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return strconv.FormatUint(uint64(stat.Dev), 10)
}

func filesystemID(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return deviceID(info), nil
}
