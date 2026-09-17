//go:build windows

package transport

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sspiCredOutbound         = 2
	sspiNativeDREP           = 0x10
	sspiBufferToken          = 2
	sspiBufferChannelBinding = 14
	sspiBufferVersion        = 0
	sspiReqConnection        = 0x00000800
	sspiReqAllocateMemory    = 0x00000100
	sspiOK                   = 0
	sspiContinueNeeded       = 0x00090312
	sspiChannelBindingsSize  = 32
)

var (
	secur32                        = windows.NewLazySystemDLL("secur32.dll")
	procAcquireCredentialsHandleW  = secur32.NewProc("AcquireCredentialsHandleW")
	procInitializeSecurityContextW = secur32.NewProc("InitializeSecurityContextW")
	procDeleteSecurityContext      = secur32.NewProc("DeleteSecurityContext")
	procFreeCredentialsHandle      = secur32.NewProc("FreeCredentialsHandle")
	procFreeContextBuffer          = secur32.NewProc("FreeContextBuffer")
)

var errSSPIContext = errors.New("transport: sspi did not produce a token")

type sspiHandle struct {
	lower uintptr
	upper uintptr
}

type secBuffer struct {
	count      uint32
	bufferType uint32
	buffer     *byte
}

type secBufferDesc struct {
	version uint32
	count   uint32
	buffers *secBuffer
}

type sspiGenerator struct {
	header   string
	target   *uint16
	bindings []byte
	cred     sspiHandle
	ctx      sspiHandle
	haveCtx  bool
	closed   bool
}

func sspiPackage(scheme string) string {
	if scheme == schemeNTLM {
		return "NTLM"
	}
	return "Negotiate"
}

func sspiChannelBindings(applicationData []byte) []byte {
	if applicationData == nil {
		return nil
	}
	out := make([]byte, sspiChannelBindingsSize, sspiChannelBindingsSize+len(applicationData))
	binary.LittleEndian.PutUint32(out[24:], uint32(len(applicationData)))
	binary.LittleEndian.PutUint32(out[28:], sspiChannelBindingsSize)
	return append(out, applicationData...)
}

func newIntegratedGenerator(scheme, host string, binding []byte) (authGenerator, bool) {
	if host == "" || (scheme != schemeNTLM && scheme != schemeNegotiate) {
		return nil, false
	}
	pkg := sspiPackage(scheme)
	pkgPtr, _ := windows.UTF16PtrFromString(pkg)
	targetPtr, err := windows.UTF16PtrFromString("HTTP/" + host)
	if err != nil {
		return nil, false
	}
	g := &sspiGenerator{header: pkg, target: targetPtr, bindings: sspiChannelBindings(binding)}
	var expiry int64
	status, _, _ := procAcquireCredentialsHandleW.Call(
		0,
		uintptr(unsafe.Pointer(pkgPtr)),
		sspiCredOutbound,
		0, 0, 0, 0,
		uintptr(unsafe.Pointer(&g.cred)),
		uintptr(unsafe.Pointer(&expiry)),
	)
	if status != sspiOK {
		return nil, false
	}
	return g, true
}

func (g *sspiGenerator) inputBuffers(serverToken []byte) []secBuffer {
	var buffers []secBuffer
	if len(serverToken) > 0 {
		buffers = append(buffers, secBuffer{count: uint32(len(serverToken)), bufferType: sspiBufferToken, buffer: &serverToken[0]})
	}
	if len(g.bindings) > 0 {
		buffers = append(buffers, secBuffer{count: uint32(len(g.bindings)), bufferType: sspiBufferChannelBinding, buffer: &g.bindings[0]})
	}
	return buffers
}

func (g *sspiGenerator) next(serverToken []byte) (string, bool, error) {
	buffers := g.inputBuffers(serverToken)
	var inDesc secBufferDesc
	var inputPtr uintptr
	if len(buffers) > 0 {
		inDesc = secBufferDesc{version: sspiBufferVersion, count: uint32(len(buffers)), buffers: &buffers[0]}
		inputPtr = uintptr(unsafe.Pointer(&inDesc))
	}
	outBuf := secBuffer{bufferType: sspiBufferToken}
	outDesc := secBufferDesc{version: sspiBufferVersion, count: 1, buffers: &outBuf}
	var ctxPtr uintptr
	if g.haveCtx {
		ctxPtr = uintptr(unsafe.Pointer(&g.ctx))
	}
	var attrs uint32
	var expiry int64
	status, _, _ := procInitializeSecurityContextW.Call(
		uintptr(unsafe.Pointer(&g.cred)),
		ctxPtr,
		uintptr(unsafe.Pointer(g.target)),
		sspiReqConnection|sspiReqAllocateMemory,
		0,
		sspiNativeDREP,
		inputPtr,
		0,
		uintptr(unsafe.Pointer(&g.ctx)),
		uintptr(unsafe.Pointer(&outDesc)),
		uintptr(unsafe.Pointer(&attrs)),
		uintptr(unsafe.Pointer(&expiry)),
	)
	g.haveCtx = true
	token := copyContextBuffer(&outBuf)
	if (status != sspiOK && status != sspiContinueNeeded) || len(token) == 0 {
		return "", true, errSSPIContext
	}
	return g.header + " " + base64.StdEncoding.EncodeToString(token), status == sspiOK, nil
}

func copyContextBuffer(buf *secBuffer) []byte {
	if buf.buffer == nil || buf.count == 0 {
		return nil
	}
	out := make([]byte, buf.count)
	copy(out, unsafe.Slice(buf.buffer, buf.count))
	_, _, _ = procFreeContextBuffer.Call(uintptr(unsafe.Pointer(buf.buffer)))
	return out
}

func (g *sspiGenerator) close() {
	if g.closed {
		return
	}
	g.closed = true
	if g.haveCtx {
		_, _, _ = procDeleteSecurityContext.Call(uintptr(unsafe.Pointer(&g.ctx)))
	}
	_, _, _ = procFreeCredentialsHandle.Call(uintptr(unsafe.Pointer(&g.cred)))
}
