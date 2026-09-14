package attributes

import "bytes"

const (
	asciiBackspace = '\b'
	asciiTab       = '\t'
	asciiEscape    = 0x1b
	asciiFormFeed  = 0x0c
	asciiDelete    = 0x7f
	asciiSubst     = 0x1a
	asciiSpace     = 0x20
	printableShift = 7
)

type textStats struct {
	nul          int
	loneCR       int
	loneLF       int
	crlf         int
	printable    int
	nonPrintable int
}

func gatherStats(data []byte) textStats {
	var stats textStats
	for at := 0; at < len(data); at++ {
		c := data[at]
		switch {
		case c == '\r' && at+1 < len(data) && data[at+1] == '\n':
			stats.crlf++
			at++
		case c == '\r':
			stats.loneCR++
		case c == '\n':
			stats.loneLF++
		case c == asciiDelete:
			stats.nonPrintable++
		case c == asciiBackspace || c == asciiTab || c == asciiEscape || c == asciiFormFeed:
			stats.printable++
		case c == 0:
			stats.nul++
			stats.nonPrintable++
		case c < asciiSpace:
			stats.nonPrintable++
		default:
			stats.printable++
		}
	}
	if len(data) > 0 && data[len(data)-1] == asciiSubst {
		stats.nonPrintable--
	}
	return stats
}

func (s textStats) binary() bool {
	return s.loneCR > 0 || s.nul > 0 || s.printable>>printableShift < s.nonPrintable
}

func hasCRLFInIndex(blob []byte) bool {
	if bytes.IndexByte(blob, '\r') < 0 {
		return false
	}
	stats := gatherStats(blob)
	return !stats.binary() && stats.crlf > 0
}
