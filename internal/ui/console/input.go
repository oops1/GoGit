package console

import (
	"github.com/oops1/headless-gui/v3/widget"
)

const XAMLTag = "ConsoleInput"

type Input struct {
	*widget.TextInput

	OnPrevious func()
	OnNext     func()
}

func NewInput() *Input {
	return &Input{TextInput: widget.NewTextInput("")}
}

func Register() {
	widget.RegisterXAMLWidget(XAMLTag, buildFromXAML)
}

func buildFromXAML(_ widget.XAMLAttrs) (widget.Widget, error) {
	return NewInput(), nil
}

func (i *Input) OnKeyEvent(e widget.KeyEvent) {
	if e.Pressed && i.recall(e.Code) {
		return
	}
	i.TextInput.OnKeyEvent(e)
}

func (i *Input) recall(code widget.KeyCode) bool {
	switch code {
	case widget.KeyUp:
		return call(i.OnPrevious)
	case widget.KeyDown:
		return call(i.OnNext)
	}
	return false
}

func call(fn func()) bool {
	if fn == nil {
		return false
	}
	fn()
	return true
}
