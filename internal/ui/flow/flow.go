package flow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/dialogs"
)

var ErrWidgetMissing = errors.New("flow: named widget missing")

var loadDialog = dialogs.Load

type Hint struct {
	Key  string
	Args []any
	OK   bool
}

func validBranchName(name string) bool {
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "//") || strings.HasPrefix(name, "-") ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".lock") {
		return false
	}
	return !strings.ContainsAny(name, " ~^:?*[\\\t")
}

func bindWidget[T widget.Widget](named map[string]widget.Widget, name string, target *T) error {
	found, ok := named[name].(T)
	if !ok {
		return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
	}
	*target = found
	return nil
}
