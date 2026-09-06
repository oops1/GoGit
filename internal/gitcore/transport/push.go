package transport

import (
	"fmt"
	"io"
	"strings"
)

func negotiatedPushCapabilities(caps Capabilities, req PushRequest, agent string) (line string, reportV2, usePushOptions bool) {
	var tokens []string
	reportV2 = caps.Has(CapReportStatusV2)
	switch {
	case reportV2:
		tokens = append(tokens, CapReportStatusV2)
	case caps.Has(CapReportStatus):
		tokens = append(tokens, CapReportStatus)
	}
	if caps.Has(CapDeleteRefs) {
		tokens = append(tokens, CapDeleteRefs)
	}
	if caps.Has(CapOfsDelta) {
		tokens = append(tokens, CapOfsDelta)
	}
	if req.Atomic && caps.Has(CapAtomic) {
		tokens = append(tokens, CapAtomic)
	}
	usePushOptions = len(req.Options) > 0 && caps.Has(CapPushOptions)
	if usePushOptions {
		tokens = append(tokens, CapPushOptions)
	}
	switch {
	case caps.Has(CapSideBand64k):
		tokens = append(tokens, CapSideBand64k)
	case caps.Has(CapSideBand):
		tokens = append(tokens, CapSideBand)
	}
	tokens = append(tokens, CapAgent+"="+agent)
	return strings.Join(tokens, " "), reportV2, usePushOptions
}

func updateCommandLine(u Update) string {
	return u.Old.String() + " " + u.New.String() + " " + u.Name
}

func buildPushRequest(req PushRequest, caps Capabilities, agent string) (body []byte, reportV2 bool) {
	capsLine, reportV2, usePushOptions := negotiatedPushCapabilities(caps, req, agent)
	for i, update := range req.Updates {
		if i == 0 {
			body = appendPktLine(body, updateCommandLine(update)+"\x00"+capsLine+"\n")
			continue
		}
		body = appendPktLine(body, updateCommandLine(update)+"\n")
	}
	body = appendFlushPkt(body)
	if usePushOptions {
		for _, opt := range req.Options {
			body = appendPktLine(body, opt+"\n")
		}
		body = appendFlushPkt(body)
	}
	return body, reportV2
}

func finishPush(body io.Reader, caps Capabilities) (*PushResult, error) {
	r := body
	if caps.Has(CapSideBand64k) || caps.Has(CapSideBand) {
		r = NewSidebandReader(body, nil)
	}
	return parseReportStatus(NewDecoder(r))
}

func parseReportStatus(dec *Decoder) (*PushResult, error) {
	line, typ, err := readOnePktLine(dec)
	if err != nil {
		return nil, err
	}
	if typ != PktData {
		return nil, fmt.Errorf("%w: missing unpack status line", ErrProtocol)
	}
	rest, ok := strings.CutPrefix(line, "unpack ")
	if !ok {
		return nil, fmt.Errorf("%w: malformed unpack status line %q", ErrProtocol, line)
	}
	result := &PushResult{}
	if rest == "ok" {
		result.UnpackOK = true
	} else {
		result.UnpackError = rest
	}
	for {
		line, typ, err := readOnePktLine(dec)
		if err != nil {
			return nil, err
		}
		if typ == PktFlush {
			return result, nil
		}
		if typ != PktData {
			continue
		}
		if err := applyReportStatusLine(result, line); err != nil {
			return nil, err
		}
	}
}

func applyReportStatusLine(result *PushResult, line string) error {
	switch {
	case strings.HasPrefix(line, "ok "):
		result.Refs = append(result.Refs, RefStatus{Name: strings.TrimPrefix(line, "ok "), OK: true})
		return nil
	case strings.HasPrefix(line, "ng "):
		rest := strings.TrimPrefix(line, "ng ")
		name, msg, ok := strings.Cut(rest, " ")
		if !ok {
			name, msg = rest, ""
		}
		result.Refs = append(result.Refs, RefStatus{Name: name, Message: msg})
		return nil
	case strings.HasPrefix(line, "option "):
		if len(result.Refs) == 0 {
			return fmt.Errorf("%w: option line %q with no preceding ref status", ErrProtocol, line)
		}
		opt := strings.TrimPrefix(line, "option ")
		last := &result.Refs[len(result.Refs)-1]
		if last.Message == "" {
			last.Message = opt
		} else {
			last.Message += "; " + opt
		}
		return nil
	default:
		return fmt.Errorf("%w: unexpected report-status line %q", ErrProtocol, line)
	}
}
