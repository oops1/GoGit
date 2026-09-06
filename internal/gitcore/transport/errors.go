package transport

import "errors"

var (
	ErrProtocol           = errors.New("transport: protocol violation")
	ErrUnsupportedScheme  = errors.New("transport: unsupported url scheme")
	ErrUnsupportedVersion = errors.New("transport: unsupported protocol version")
	ErrNoCredentials      = errors.New("transport: no credentials for the resource")
	ErrAuthRequired       = errors.New("transport: authentication required")
	ErrAccessDenied       = errors.New("transport: access denied")
	ErrRepositoryNotFound = errors.New("transport: repository not found")
	ErrEmptyRepository    = errors.New("transport: repository is empty")

	ErrPktLineBadLength = errors.New("transport: pkt-line length is not hexadecimal")
	ErrPktLineReserved  = errors.New("transport: pkt-line length 0003 is reserved")
	ErrPktLineTooLong   = errors.New("transport: pkt-line data exceeds the maximum length")
	ErrPktLineTruncated = errors.New("transport: pkt-line stream ended mid-packet")

	ErrSidebandChannel = errors.New("transport: unknown side-band channel")
	ErrSidebandRemote  = errors.New("transport: remote reported an error")

	ErrAdvertisementMalformed = errors.New("transport: malformed reference advertisement")

	ErrInvalidURL = errors.New("transport: invalid url")
)
