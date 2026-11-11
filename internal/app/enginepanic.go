package app

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/oops1/headless-gui/v4/engine"
	"github.com/oops1/headless-gui/v4/widget"

	"github.com/oops1/gogit/internal/logx"
)

type enginePanicSite struct {
	where  string
	widget string
}

type enginePanicLog struct {
	mu   sync.Mutex
	seen map[enginePanicSite]bool
}

func (l *enginePanicLog) firstAt(site enginePanicSite) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[site] {
		return false
	}
	if l.seen == nil {
		l.seen = map[enginePanicSite]bool{}
	}
	l.seen[site] = true
	return true
}

func (a *App) reportEnginePanic(info engine.PanicInfo) {
	site := enginePanicSite{where: info.Where, widget: widgetTypeName(info.Widget)}
	if !a.enginePanics.firstAt(site) {
		return
	}
	a.log.Error("engine caught a panic",
		"where", info.Where,
		"widget", site.widget,
		"path", info.Path,
		"value", logx.RedactText(fmt.Sprint(info.Value)),
		"stack", logx.RedactText(string(info.Stack)))
}

func widgetTypeName(w widget.Widget) string {
	if w == nil {
		return ""
	}
	return reflect.TypeOf(w).String()
}
