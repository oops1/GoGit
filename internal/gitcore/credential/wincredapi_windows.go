package credential

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric             = 1
	credPersistLocalMachine     = 2
	credEnumerateAllCredentials = 1
)

type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32           = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW      = advapi32.NewProc("CredReadW")
	procCredWriteW     = advapi32.NewProc("CredWriteW")
	procCredDeleteW    = advapi32.NewProc("CredDeleteW")
	procCredEnumerateW = advapi32.NewProc("CredEnumerateW")
	procCredFree       = advapi32.NewProc("CredFree")
)

type advapiCredentials struct {
	credRead      func(target *uint16, cred **credentialW) error
	credWrite     func(cred *credentialW) error
	credDelete    func(target *uint16) error
	credEnumerate func(filter *uint16, flags uint32, count *uint32, creds ***credentialW) error
	credFree      func(buffer unsafe.Pointer)
}

func newAdvapiCredentials() *advapiCredentials {
	return &advapiCredentials{
		credRead:      callCredRead,
		credWrite:     callCredWrite,
		credDelete:    callCredDelete,
		credEnumerate: callCredEnumerate,
		credFree:      callCredFree,
	}
}

func credCallResult(r uintptr, err error) error {
	if r == 0 {
		return err
	}
	return nil
}

func callCredRead(target *uint16, cred **credentialW) error {
	r, _, err := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(cred)))
	return credCallResult(r, err)
}

func callCredWrite(cred *credentialW) error {
	r, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(cred)), 0)
	return credCallResult(r, err)
}

func callCredDelete(target *uint16) error {
	r, _, err := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	return credCallResult(r, err)
}

func callCredEnumerate(filter *uint16, flags uint32, count *uint32, creds ***credentialW) error {
	r, _, err := procCredEnumerateW.Call(uintptr(unsafe.Pointer(filter)), uintptr(flags), uintptr(unsafe.Pointer(count)), uintptr(unsafe.Pointer(creds)))
	return credCallResult(r, err)
}

func callCredFree(buffer unsafe.Pointer) {
	_, _, _ = procCredFree.Call(uintptr(buffer))
}

func optionalUTF16Ptr(s string) (*uint16, error) {
	if s == "" {
		return nil, nil
	}
	return windows.UTF16PtrFromString(s)
}

func winCredentialFrom(c *credentialW) winCredential {
	return winCredential{
		target:   windows.UTF16PtrToString(c.TargetName),
		userName: windows.UTF16PtrToString(c.UserName),
		comment:  windows.UTF16PtrToString(c.Comment),
		blob:     append([]byte(nil), unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)...),
	}
}

func (a *advapiCredentials) enumerate(filter string) ([]winCredential, error) {
	filterPtr, err := optionalUTF16Ptr(filter)
	if err != nil {
		return nil, err
	}
	var flags uint32
	if filterPtr == nil {
		flags = credEnumerateAllCredentials
	}
	var (
		count uint32
		list  **credentialW
	)
	if err := a.credEnumerate(filterPtr, flags, &count, &list); err != nil {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return nil, nil
		}
		return nil, fmt.Errorf("credential: list windows credentials: %w", err)
	}
	defer a.credFree(unsafe.Pointer(list))
	out := make([]winCredential, 0, count)
	for _, c := range unsafe.Slice(list, count) {
		if c.Type == credTypeGeneric {
			out = append(out, winCredentialFrom(c))
		}
	}
	return out, nil
}

func (a *advapiCredentials) read(target string) (winCredential, bool, error) {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return winCredential{}, false, err
	}
	var cred *credentialW
	if err := a.credRead(targetPtr, &cred); err != nil {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return winCredential{}, false, nil
		}
		return winCredential{}, false, fmt.Errorf("credential: read windows credential: %w", err)
	}
	defer a.credFree(unsafe.Pointer(cred))
	return winCredentialFrom(cred), true, nil
}

func (a *advapiCredentials) write(cred winCredential) error {
	target, err := windows.UTF16PtrFromString(cred.target)
	if err != nil {
		return err
	}
	userName, err := optionalUTF16Ptr(cred.userName)
	if err != nil {
		return err
	}
	comment, err := optionalUTF16Ptr(cred.comment)
	if err != nil {
		return err
	}
	native := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		Comment:            comment,
		CredentialBlobSize: uint32(len(cred.blob)),
		Persist:            credPersistLocalMachine,
		UserName:           userName,
	}
	if len(cred.blob) > 0 {
		native.CredentialBlob = &cred.blob[0]
	}
	if err := a.credWrite(&native); err != nil {
		return fmt.Errorf("credential: write windows credential: %w", err)
	}
	return nil
}

func (a *advapiCredentials) remove(target string) error {
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	if err := a.credDelete(targetPtr); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("credential: delete windows credential: %w", err)
	}
	return nil
}
