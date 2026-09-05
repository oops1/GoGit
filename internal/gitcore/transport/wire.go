package transport

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/progress"
)

const maxHavesPerRound = 32

const defaultAgent = "gogit"

func agentValue(opts Options) string {
	if opts.UserAgent != "" {
		return opts.UserAgent
	}
	return defaultAgent
}

func resourceOf(endpoint Endpoint) string {
	return endpoint.Host + endpoint.Path
}

type ackStatus int

const (
	ackNAK ackStatus = iota
	ackBare
	ackContinue
	ackCommon
	ackReady
)

type ackLine struct {
	id     hash.ObjectID
	status ackStatus
}

func parseAckLine(line string) (ackLine, error) {
	if line == "NAK" {
		return ackLine{status: ackNAK}, nil
	}
	rest, ok := strings.CutPrefix(line, "ACK ")
	if !ok {
		return ackLine{}, fmt.Errorf("%w: unexpected negotiation line %q", ErrProtocol, line)
	}
	oidText, status, _ := strings.Cut(rest, " ")
	id, err := hash.Parse(oidText)
	if err != nil {
		return ackLine{}, fmt.Errorf("%w: %w", ErrProtocol, err)
	}
	switch status {
	case "":
		return ackLine{id: id, status: ackBare}, nil
	case "continue":
		return ackLine{id: id, status: ackContinue}, nil
	case "common":
		return ackLine{id: id, status: ackCommon}, nil
	case "ready":
		return ackLine{id: id, status: ackReady}, nil
	default:
		return ackLine{}, fmt.Errorf("%w: unknown ack status %q", ErrProtocol, status)
	}
}

func applyAck(neg Negotiator, ack ackLine) {
	if neg == nil || ack.id.IsZero() {
		return
	}
	switch ack.status {
	case ackBare, ackContinue, ackCommon, ackReady:
		neg.Common(ack.id)
	}
}

func negHaves(ctx context.Context, neg Negotiator) iter.Seq2[hash.ObjectID, error] {
	if neg == nil {
		return func(func(hash.ObjectID, error) bool) {}
	}
	return neg.Haves(ctx)
}

func pullHaves(next func() (hash.ObjectID, error, bool), limit int) (batch []hash.ObjectID, exhausted bool, err error) {
	for len(batch) < limit {
		id, e, ok := next()
		if !ok {
			return batch, true, nil
		}
		if e != nil {
			return batch, false, e
		}
		batch = append(batch, id)
	}
	return batch, false, nil
}

func haveIterFunc(seq iter.Seq2[hash.ObjectID, error]) func() (hash.ObjectID, error, bool) {
	next, stop := iter.Pull2(seq)
	stopped := false
	return func() (hash.ObjectID, error, bool) {
		if stopped {
			return hash.Zero, nil, false
		}
		id, err, ok := next()
		if !ok {
			stopped = true
			stop()
		}
		return id, err, ok
	}
}

func appendPktLine(dst []byte, text string) []byte {
	var header [4]byte
	encodePktLength(header[:], len(text)+4)
	dst = append(dst, header[:]...)
	dst = append(dst, text...)
	return dst
}

func appendFlushPkt(dst []byte) []byte {
	return append(dst, "0000"...)
}

func appendDelimPkt(dst []byte) []byte {
	return append(dst, "0001"...)
}

func appendDonePkt(dst []byte) []byte {
	return appendPktLine(dst, "done\n")
}

func readOnePktLine(dec *Decoder) (string, PktType, error) {
	if !dec.Scan() {
		if err := dec.Err(); err != nil {
			return "", PktFlush, err
		}
		return "", PktFlush, fmt.Errorf("%w: connection closed while awaiting a response", ErrProtocol)
	}
	if dec.Type() != PktData {
		return "", dec.Type(), nil
	}
	return strings.TrimSuffix(string(dec.Bytes()), "\n"), PktData, nil
}

func readShallowUpdate(dec *Decoder) (shallow, unshallow []hash.ObjectID, err error) {
	for {
		line, typ, err := readOnePktLine(dec)
		if err != nil {
			return nil, nil, err
		}
		if typ == PktFlush {
			return shallow, unshallow, nil
		}
		if typ != PktData {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "shallow "); ok {
			id, err := hash.Parse(rest)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %w", ErrProtocol, err)
			}
			shallow = append(shallow, id)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "unshallow "); ok {
			id, err := hash.Parse(rest)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %w", ErrProtocol, err)
			}
			unshallow = append(unshallow, id)
			continue
		}
		return nil, nil, fmt.Errorf("%w: unexpected line %q in shallow update", ErrProtocol, line)
	}
}

func hasDeepenArgs(req FetchRequest) bool {
	return req.Depth > 0 || !req.DeepenSince.IsZero() || len(req.DeepenNot) > 0
}

func appendDeepenArgs(dst []byte, req FetchRequest, lineFmt func([]byte, string) []byte) []byte {
	if req.Depth > 0 {
		dst = lineFmt(dst, "deepen "+strconv.Itoa(req.Depth)+"\n")
	}
	if !req.DeepenSince.IsZero() {
		dst = lineFmt(dst, "deepen-since "+strconv.FormatInt(req.DeepenSince.Unix(), 10)+"\n")
	}
	for _, ref := range req.DeepenNot {
		dst = lineFmt(dst, "deepen-not "+ref+"\n")
	}
	return dst
}

type readCloser struct {
	io.Reader
	closer func() error
}

func (r readCloser) Close() error {
	return r.closer()
}

func wrapPack(reader io.ReadCloser, caps Capabilities, prog progress.Func) io.ReadCloser {
	if caps.Has(CapSideBand) || caps.Has(CapSideBand64k) {
		return readCloser{Reader: NewSidebandReader(reader, prog), closer: reader.Close}
	}
	return reader
}

func closeQuietly(c io.Closer) {
	_ = c.Close()
}
