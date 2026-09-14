package attributes

import "bytes"

type IndexBlob func() ([]byte, bool)

func (p TextPolicy) guesses() bool {
	switch p.Effective {
	case CRLFAuto, CRLFAutoInput, CRLFAutoCRLF:
		return true
	}
	return false
}

func (p TextPolicy) ToGit(data []byte, index IndexBlob) []byte {
	if p.Effective == CRLFBinary || len(data) == 0 {
		return data
	}
	stats := gatherStats(data)
	if stats.crlf == 0 {
		return data
	}
	if p.guesses() {
		if stats.binary() || indexHoldsCRLF(index) {
			return data
		}
		return bytes.ReplaceAll(data, []byte("\r"), nil)
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func indexHoldsCRLF(index IndexBlob) bool {
	if index == nil {
		return false
	}
	blob, ok := index()
	return ok && hasCRLFInIndex(blob)
}

func (p TextPolicy) willConvertLFToCRLF(stats textStats) bool {
	if p.Convert.OnCheckout != ConvertCRLF || stats.loneLF == 0 {
		return false
	}
	return !p.guesses() || stats.loneCR == 0 && stats.crlf == 0 && !stats.binary()
}

func (p TextPolicy) ToWorkingTree(data []byte) []byte {
	if len(data) == 0 || p.Convert.OnCheckout != ConvertCRLF {
		return data
	}
	stats := gatherStats(data)
	if !p.willConvertLFToCRLF(stats) {
		return data
	}
	out := make([]byte, 0, len(data)+stats.loneLF)
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		if nl > 0 && data[nl-1] == '\r' {
			out = append(out, data[:nl+1]...)
		} else {
			out = append(out, data[:nl]...)
			out = append(out, '\r', '\n')
		}
		data = data[nl+1:]
	}
	return append(out, data...)
}
