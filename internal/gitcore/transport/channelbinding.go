package transport

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"hash"
)

const tlsServerEndPointPrefix = "tls-server-end-point:"

func certificateBindingHash(alg x509.SignatureAlgorithm) hash.Hash {
	switch alg {
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384, x509.SHA384WithRSAPSS:
		return sha512.New384()
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512, x509.SHA512WithRSAPSS:
		return sha512.New()
	default:
		return sha256.New()
	}
}

func tlsServerEndPoint(cert *x509.Certificate) []byte {
	h := certificateBindingHash(cert.SignatureAlgorithm)
	_, _ = h.Write(cert.Raw)
	return append([]byte(tlsServerEndPointPrefix), h.Sum(nil)...)
}

func channelBindingFromState(state *tls.ConnectionState) []byte {
	if state == nil || len(state.PeerCertificates) == 0 {
		return nil
	}
	return tlsServerEndPoint(state.PeerCertificates[0])
}

func gssChannelBindings(applicationData []byte) []byte {
	out := make([]byte, 20, 20+len(applicationData))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(applicationData)))
	return append(out, applicationData...)
}

func ntlmChannelBindingHash(applicationData []byte) []byte {
	if applicationData == nil {
		return nil
	}
	sum := md5.Sum(gssChannelBindings(applicationData))
	return sum[:]
}
