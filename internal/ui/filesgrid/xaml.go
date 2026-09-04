package filesgrid

import (
	"github.com/oops1/headless-gui/v3/widget"
)

const XAMLTag = "FilesGrid"

func Register() {
	widget.RegisterXAMLWidget(XAMLTag, buildFromXAML)
}

func buildFromXAML(_ widget.XAMLAttrs) (widget.Widget, error) {
	return New(), nil
}
