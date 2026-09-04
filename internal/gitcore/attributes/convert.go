package attributes

import (
	"bytes"
	"os"
	"runtime"
	"strings"
)

const (
	AutoCRLFFalse = "false"
	AutoCRLFTrue  = "true"
	AutoCRLFInput = "input"

	EOLNative = "native"
	EOLLF     = "lf"
	EOLCRLF   = "crlf"

	binarySampleSize = 8000
)

type CRLFAction uint8

const (
	CRLFUndefined CRLFAction = iota
	CRLFBinary
	CRLFText
	CRLFTextInput
	CRLFTextCRLF
	CRLFAuto
	CRLFAutoInput
	CRLFAutoCRLF
)

func (a CRLFAction) String() string {
	switch a {
	case CRLFBinary:
		return "-text"
	case CRLFText:
		return "text"
	case CRLFTextInput:
		return "text eol=lf"
	case CRLFTextCRLF:
		return "text eol=crlf"
	case CRLFAuto:
		return "text=auto"
	case CRLFAutoInput:
		return "text=auto eol=lf"
	case CRLFAutoCRLF:
		return "text=auto eol=crlf"
	}
	return ""
}

type Conversion uint8

const (
	ConvertNone Conversion = iota
	ConvertLF
	ConvertCRLF
)

func (c Conversion) String() string {
	switch c {
	case ConvertLF:
		return "lf"
	case ConvertCRLF:
		return "crlf"
	}
	return "none"
}

type Convert struct {
	OnCheckout Conversion
	OnCheckin  Conversion
	Detect     bool
}

type TextPolicy struct {
	Attr      CRLFAction
	Effective CRLFAction
	Convert   Convert
}

func crlfFromValue(v Value) CRLFAction {
	switch {
	case v.IsSet():
		return CRLFText
	case v.IsUnset():
		return CRLFBinary
	case v.kind == Valued && v.text == "auto":
		return CRLFAuto
	}
	return CRLFUndefined
}

func eolFromValue(v Value) Conversion {
	if v.kind != Valued {
		return ConvertNone
	}
	switch v.text {
	case EOLLF:
		return ConvertLF
	case EOLCRLF:
		return ConvertCRLF
	}
	return ConvertNone
}

func (a *Attributes) Text(path string) TextPolicy {
	values := a.Get(path, "text", "crlf", "eol")
	action := crlfFromValue(values["text"])
	if action == CRLFUndefined {
		action = crlfFromValue(values["crlf"])
	}
	if action != CRLFBinary {
		action = applyEOLAttribute(action, eolFromValue(values["eol"]))
	}
	nativeCRLF := a.textEOLIsCRLF()
	effective := action
	switch {
	case effective == CRLFText && nativeCRLF:
		effective = CRLFTextCRLF
	case effective == CRLFText:
		effective = CRLFTextInput
	}
	if effective == CRLFUndefined {
		effective = undefinedAction(a.opts.AutoCRLF)
	}
	return TextPolicy{
		Attr:      action,
		Effective: effective,
		Convert:   conversionFor(effective, nativeCRLF),
	}
}

func applyEOLAttribute(action CRLFAction, eol Conversion) CRLFAction {
	switch {
	case eol == ConvertLF && action == CRLFAuto:
		return CRLFAutoInput
	case eol == ConvertCRLF && action == CRLFAuto:
		return CRLFAutoCRLF
	case eol == ConvertLF:
		return CRLFTextInput
	case eol == ConvertCRLF:
		return CRLFTextCRLF
	}
	return action
}

func undefinedAction(autoCRLF string) CRLFAction {
	switch autoCRLF {
	case AutoCRLFTrue:
		return CRLFAutoCRLF
	case AutoCRLFInput:
		return CRLFAutoInput
	}
	return CRLFBinary
}

func conversionFor(action CRLFAction, nativeCRLF bool) Convert {
	if action == CRLFBinary {
		return Convert{}
	}
	out := Convert{
		OnCheckin: ConvertLF,
		Detect:    action == CRLFAuto || action == CRLFAutoInput || action == CRLFAutoCRLF,
	}
	switch action {
	case CRLFTextInput, CRLFAutoInput:
		out.OnCheckout = ConvertLF
	case CRLFTextCRLF, CRLFAutoCRLF:
		out.OnCheckout = ConvertCRLF
	case CRLFText, CRLFAuto, CRLFUndefined:
		out.OnCheckout = ConvertLF
		if nativeCRLF {
			out.OnCheckout = ConvertCRLF
		}
	}
	return out
}

func (a *Attributes) textEOLIsCRLF() bool {
	switch a.opts.AutoCRLF {
	case AutoCRLFTrue:
		return true
	case AutoCRLFInput:
		return false
	}
	switch a.opts.EOL {
	case EOLCRLF:
		return true
	case EOLLF:
		return false
	}
	return platformIsCRLF(a.opts.PlatformEOL)
}

func platformIsCRLF(platform string) bool {
	switch platform {
	case EOLCRLF:
		return true
	case EOLLF:
		return false
	}
	return runtime.GOOS == "windows"
}

func IsBinaryContent(sample []byte) bool {
	if len(sample) > binarySampleSize {
		sample = sample[:binarySampleSize]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

func DefaultExcludesFile(env func(string) string) string {
	return xdgConfigFile(env, "ignore")
}

func DefaultAttributesFile(env func(string) string) string {
	return xdgConfigFile(env, "attributes")
}

func slashDir(dir string) string {
	return strings.TrimRight(strings.ReplaceAll(dir, `\`, "/"), "/")
}

func xdgConfigFile(env func(string) string, name string) string {
	if env == nil {
		env = os.Getenv
	}
	if dir := env("XDG_CONFIG_HOME"); dir != "" {
		return slashDir(dir) + "/git/" + name
	}
	home := env("HOME")
	if home == "" {
		home = env("USERPROFILE")
	}
	if home == "" {
		return ""
	}
	return slashDir(home) + "/.config/git/" + name
}
