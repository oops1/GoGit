package dialogs

import (
	"testing"

	"github.com/oops1/headless-gui/v4/widget"
)

func sizedForTest() *widget.Dialog {
	dlg := widget.NewDialog("Title", 500, 400)
	dlg.SetResizable(true)
	dlg.SetMinSize(300, 200)
	return dlg
}

func TestARememberedSizeIsRestored(t *testing.T) {
	dlg := sizedForTest()

	RestoreSize(dlg, 640, 480)

	if w, h, ok := RememberedSize(dlg); !ok || w != 640 || h != 480 {
		t.Fatalf("size = %dx%d, %v, want 640x480", w, h, ok)
	}
}

func TestNoRememberedSizeKeepsTheDesignedOne(t *testing.T) {
	for _, tt := range []struct {
		name          string
		width, height int
	}{
		{"Nothing", 0, 0},
		{"NoWidth", 0, 480},
		{"NoHeight", 640, 0},
		{"Negative", -5, 480},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dlg := sizedForTest()

			RestoreSize(dlg, tt.width, tt.height)

			if w, h, _ := RememberedSize(dlg); w != 500 || h != 400 {
				t.Fatalf("size = %dx%d, want the designed 500x400", w, h)
			}
		})
	}
}

func TestATooSmallSizeStopsAtTheMinimum(t *testing.T) {
	dlg := sizedForTest()

	RestoreSize(dlg, 10, 10)

	if w, h, _ := RememberedSize(dlg); w != 300 || h != 200 {
		t.Fatalf("size = %dx%d, want the minimum 300x200", w, h)
	}
}

func TestAMaximizedWindowRemembersNoSize(t *testing.T) {
	dlg := sizedForTest()
	dlg.SetMaximized(true)

	if _, _, ok := RememberedSize(dlg); ok {
		t.Fatal("a maximized window must not overwrite the size to return to")
	}
}
