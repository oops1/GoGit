package style

import "github.com/oops1/headless-gui/v4/engine"

func TextQuality() engine.TextQuality {
	return engine.TextQuality{Hinting: true, Contrast: engine.DefaultTextContrast}
}
