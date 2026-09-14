package attributes

import "bytes"

const (
	identKeyword  = "Id"
	identExpanded = "Id:"
)

func countIdent(data []byte) int {
	count := 0
	for len(data) > 0 {
		c := data[0]
		data = data[1:]
		if c != '$' {
			continue
		}
		if len(data) < len(identExpanded) {
			break
		}
		if !bytes.HasPrefix(data, []byte(identKeyword)) {
			continue
		}
		c = data[2]
		data = data[3:]
		if c == '$' {
			count++
		}
		if c != ':' {
			continue
		}
		for len(data) > 0 {
			c = data[0]
			data = data[1:]
			if c == '$' {
				count++
				break
			}
			if c == '\n' {
				break
			}
		}
	}
	return count
}

func identToGit(data []byte) []byte {
	if countIdent(data) == 0 {
		return data
	}
	out := make([]byte, 0, len(data))
	for {
		dollar := bytes.IndexByte(data, '$')
		if dollar < 0 {
			break
		}
		out = append(out, data[:dollar+1]...)
		data = data[dollar+1:]
		if len(data) <= len(identExpanded) || !bytes.HasPrefix(data, []byte(identExpanded)) {
			continue
		}
		end := bytes.IndexByte(data[len(identExpanded):], '$')
		if end < 0 {
			break
		}
		end += len(identExpanded)
		if bytes.IndexByte(data[len(identExpanded):end], '\n') >= 0 {
			continue
		}
		out = append(out, identKeyword+"$"...)
		data = data[end+1:]
	}
	return append(out, data...)
}

func identToWorkingTree(data []byte, id string) []byte {
	count := countIdent(data)
	if count == 0 {
		return data
	}
	out := make([]byte, 0, len(data)+count*(len(id)+len(identExpanded)))
	for {
		dollar := bytes.IndexByte(data, '$')
		if dollar < 0 {
			break
		}
		out = append(out, data[:dollar+1]...)
		data = data[dollar+1:]
		rest, ok, stop := skipIdent(data)
		if stop {
			break
		}
		if !ok {
			continue
		}
		data = rest
		out = append(out, "Id: "+id+" $"...)
	}
	return append(out, data...)
}

func skipIdent(data []byte) ([]byte, bool, bool) {
	if len(data) < len(identExpanded) || !bytes.HasPrefix(data, []byte(identKeyword)) {
		return nil, false, false
	}
	switch data[2] {
	case '$':
		return data[3:], true, false
	case ':':
	default:
		return nil, false, false
	}
	end := bytes.IndexByte(data[len(identExpanded):], '$')
	if end < 0 {
		return nil, false, true
	}
	end += len(identExpanded)
	if bytes.IndexByte(data[len(identExpanded):end], '\n') >= 0 {
		return nil, false, false
	}
	if end > len(identExpanded) {
		if space := bytes.IndexByte(data[len(identExpanded)+1:end], ' '); space >= 0 && len(identExpanded)+1+space < end-1 {
			return nil, false, false
		}
	}
	return data[end+1:], true, false
}
