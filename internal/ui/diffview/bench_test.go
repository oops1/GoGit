package diffview

import (
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
)

func BenchmarkDrawLargeDocument(b *testing.B) {
	eng := engine.New(1200, 800, 30)
	defer eng.Stop()
	dv := New()
	dv.SetFontFamily("")
	dv.SetDocument(longDocument(5000))
	eng.SetRoot(dv)
	b.ReportAllocs()
	step := 0
	for b.Loop() {
		step = (step + 1) % 200
		dv.scrollTo(0, step*defaultRowHeight)
		eng.RenderFrameNow()
	}
}
