package transport

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const emptyAdvertisementRef = "capabilities^{}"

func ParseAdvertisement(r io.Reader) (Advertisement, error) {
	dec := NewDecoder(r)
	if !dec.Scan() {
		if err := dec.Err(); err != nil {
			return Advertisement{}, err
		}
		return Advertisement{}, fmt.Errorf("%w: advertisement stream is empty", ErrAdvertisementMalformed)
	}
	line := strings.TrimSuffix(string(dec.Bytes()), "\n")
	switch line {
	case "version 2":
		return parseV2Advertisement(dec)
	case "version 1":
		if !dec.Scan() {
			if err := dec.Err(); err != nil {
				return Advertisement{}, err
			}
			return Advertisement{}, fmt.Errorf("%w: no refs after the version 1 marker", ErrAdvertisementMalformed)
		}
		return parseV1Advertisement(1, dec.Bytes(), dec)
	default:
		return parseV1Advertisement(0, dec.Bytes(), dec)
	}
}

func parseV1Advertisement(version int, firstLine []byte, dec *Decoder) (Advertisement, error) {
	name, oidText, capsText, err := splitFirstRefLine(firstLine)
	if err != nil {
		return Advertisement{}, err
	}
	adv := Advertisement{Version: version, Capabilities: ParseCapabilities(capsText)}
	if name != emptyAdvertisementRef {
		id, err := hash.Parse(oidText)
		if err != nil {
			return Advertisement{}, fmt.Errorf("%w: %w", ErrAdvertisementMalformed, err)
		}
		adv.Refs = append(adv.Refs, Ref{Name: name, ID: id})
	}
	for dec.Scan() {
		if dec.Type() == PktFlush {
			return finishV1Advertisement(adv)
		}
		if dec.Type() != PktData {
			continue
		}
		line := strings.TrimSuffix(string(dec.Bytes()), "\n")
		if err := appendAdvertisedLine(&adv, line); err != nil {
			return Advertisement{}, err
		}
	}
	if err := dec.Err(); err != nil {
		return Advertisement{}, err
	}
	return Advertisement{}, fmt.Errorf("%w: missing flush at the end of the advertisement", ErrAdvertisementMalformed)
}

func splitFirstRefLine(line []byte) (name, oidText, capsText string, err error) {
	nul := bytes.IndexByte(line, 0)
	if nul < 0 {
		return "", "", "", fmt.Errorf("%w: the first advertised line has no capability separator", ErrAdvertisementMalformed)
	}
	head := string(line[:nul])
	capsText = strings.TrimSuffix(string(line[nul+1:]), "\n")
	oidText, name, ok := strings.Cut(head, " ")
	if !ok {
		return "", "", "", fmt.Errorf("%w: the first advertised line %q has no object id", ErrAdvertisementMalformed, head)
	}
	return name, oidText, capsText, nil
}

func appendAdvertisedLine(adv *Advertisement, line string) error {
	if rest, ok := strings.CutPrefix(line, "shallow "); ok {
		id, err := hash.Parse(rest)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrAdvertisementMalformed, err)
		}
		adv.Shallow = append(adv.Shallow, id)
		return nil
	}
	oidText, name, ok := strings.Cut(line, " ")
	if !ok {
		return fmt.Errorf("%w: advertised line %q has no object id", ErrAdvertisementMalformed, line)
	}
	if peeledName, isPeeled := strings.CutSuffix(name, "^{}"); isPeeled {
		return applyPeeled(adv, peeledName, oidText)
	}
	id, err := hash.Parse(oidText)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAdvertisementMalformed, err)
	}
	adv.Refs = append(adv.Refs, Ref{Name: name, ID: id})
	return nil
}

func applyPeeled(adv *Advertisement, name, oidText string) error {
	id, err := hash.Parse(oidText)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAdvertisementMalformed, err)
	}
	for i := len(adv.Refs) - 1; i >= 0; i-- {
		if adv.Refs[i].Name == name {
			adv.Refs[i].Peeled = id
			return nil
		}
	}
	return fmt.Errorf("%w: peeled entry for %q has no matching ref", ErrAdvertisementMalformed, name)
}

func finishV1Advertisement(adv Advertisement) (Advertisement, error) {
	for _, value := range adv.Capabilities.Values(CapSymref) {
		name, target, ok := strings.Cut(value, ":")
		if !ok {
			return Advertisement{}, fmt.Errorf("%w: malformed symref capability %q", ErrAdvertisementMalformed, value)
		}
		if name == "HEAD" {
			adv.Head = target
		}
		for i := range adv.Refs {
			if adv.Refs[i].Name == name {
				adv.Refs[i].Symref = target
				break
			}
		}
	}
	return adv, nil
}

func parseV2Advertisement(dec *Decoder) (Advertisement, error) {
	adv := Advertisement{Version: 2}
	for dec.Scan() {
		if dec.Type() == PktFlush {
			return adv, nil
		}
		if dec.Type() != PktData {
			continue
		}
		token := strings.TrimSuffix(string(dec.Bytes()), "\n")
		adv.Capabilities.Add(token)
	}
	if err := dec.Err(); err != nil {
		return Advertisement{}, err
	}
	return Advertisement{}, fmt.Errorf("%w: missing flush after the v2 capability list", ErrAdvertisementMalformed)
}
