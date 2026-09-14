package flow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/refs"
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
	return !strings.HasPrefix(name, "-") && refs.CheckFormat(refs.HeadsPrefix+name, 0) == nil
}

func validTagName(name string) bool {
	return !strings.HasPrefix(name, "-") && refs.CheckFormat(refs.TagsPrefix+name, 0) == nil
}

func bindWidget[T widget.Widget](named map[string]widget.Widget, name string, target *T) error {
	found, ok := named[name].(T)
	if !ok {
		return fmt.Errorf("%w: %s", ErrWidgetMissing, name)
	}
	*target = found
	return nil
}
