package main

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/workspace"
)

func TestCobaltWindowSuppliesEmbeddedIcon(t *testing.T) {
	icon := newConfig(newModel()).Icon
	if icon.Size < 512 || len(icon.RGBA) != icon.Size*icon.Size*4 {
		t.Fatalf("running window has no production icon: size=%d bytes=%d", icon.Size, len(icon.RGBA))
	}
}

func TestCobaltChromeUsesVectorGlyphs(t *testing.T) {
	_, driver := testDriver(t)
	for _, text := range driver.Snapshot().Texts {
		switch text.Value {
		case "☰", "⌕", "↻", "▣", "✎", "⌫", "×":
			t.Errorf("chrome still paints font glyph %q", text.Value)
		}
	}
}

func TestCobaltModesDisableUnsupportedDocumentKinds(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Kind = workspace.PlainKind
	if err := driver.Resize(740, 500); err != nil {
		t.Fatal(err)
	}
	for _, id := range []gfx.ID{renderedModeID, splitModeID, readModeID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				found = true
				if !hit.Disabled {
					t.Errorf("unsupported mode %v is enabled", id)
				}
			}
		}
		if !found {
			t.Errorf("mode %v disappeared instead of exposing disabled state", id)
		}
	}
}

func TestCobaltFileMenuArrowNavigationAndActivation(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, fileMenuID)
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != menuNewID {
		t.Fatalf("Home focus=%v, want new document", driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != noteTemplateID {
		t.Fatalf("Down focus=%v, want note template", driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.fileMenuOpen || len(state.session.Tabs) != 2 {
		t.Fatalf("template keyboard activation: menu=%v tabs=%d", state.fileMenuOpen, len(state.session.Tabs))
	}
}

func TestCobaltTitlebarActionsDoNotOverlap(t *testing.T) {
	for _, width := range []int{740, 999, 1000, 1200, 1605} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%v", width, dark), func(t *testing.T) {
				state := newModel()
				var canvas gfx.Canvas
				driver, err := app.NewDriver(newConfig(state), &canvas)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(driver.Close)
				state.dark = dark
				if err := driver.Resize(width, 500); err != nil {
					t.Fatal(err)
				}
				var actions []gfx.Hit
				actionIDs := map[gfx.ID]bool{
					sidebarToggleID: true, fileMenuID: true, newFileID: true, findDocumentID: true,
					renderedModeID: true, editModeID: true, splitModeID: true, readModeID: true,
					outlineToggleID: true, themeToggleID: true, settingsToggleID: true,
				}
				leading, trailing := canvas.TitlebarInsets()
				for _, hit := range driver.Snapshot().Hits {
					if actionIDs[hit.ID] {
						actions = append(actions, hit)
						if hit.Bounds.X < leading || hit.Bounds.X+hit.Bounds.Width > float32(width)-trailing || hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > 32 {
							t.Errorf("action %q outside safe titlebar: %+v", hit.Label, hit.Bounds)
						}
					}
				}
				if len(actions) != len(actionIDs) {
					t.Fatalf("titlebar actions=%d, want %d", len(actions), len(actionIDs))
				}
				for index, action := range actions {
					for _, other := range actions[index+1:] {
						if action.Bounds.X < other.Bounds.X+other.Bounds.Width && other.Bounds.X < action.Bounds.X+action.Bounds.Width {
							t.Errorf("overlapping actions %q and %q", action.Label, other.Label)
						}
					}
				}
			})
		}
	}
}

func TestCobaltWorkspaceSearchUsesVectorBackButton(t *testing.T) {
	_, driver := testDriver(t)
	clickID(t, driver, workspaceOpenID)
	for _, text := range driver.Snapshot().Texts {
		if text.Value == "←" {
			t.Fatal("workspace search still paints a font back arrow")
		}
	}
}

func TestCobaltMenuDismissalDoesNotActivateBackground(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, fileMenuID)
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.fileMenuOpen || driver.Snapshot().Focused != fileMenuID {
		t.Fatalf("Escape menu=%v focus=%v", state.fileMenuOpen, driver.Snapshot().Focused)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, newFileID)
	if state.fileMenuOpen || len(state.session.Tabs) != 1 {
		t.Fatalf("outside dismissal activated background: menu=%v tabs=%d", state.fileMenuOpen, len(state.session.Tabs))
	}
}

func TestCobaltMenuSkipsDisabledExports(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Kind = workspace.PlainKind
	clickID(t, driver, fileMenuID)
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != recentMenuID {
		t.Fatalf("End focus=%v", driver.Snapshot().Focused)
	}
	for _, want := range []gfx.ID{menuSettingsID, saveAsID} {
		if err := driver.Key(gfx.KeyUp, 0); err != nil {
			t.Fatal(err)
		}
		if driver.Snapshot().Focused != want {
			t.Fatalf("Up focus=%v, want %v", driver.Snapshot().Focused, want)
		}
	}
	for _, hit := range driver.Snapshot().Hits {
		if (hit.ID == menuExportHTMLID || hit.ID == menuExportPDFID || hit.ID == menuExportDOCXID) && !hit.Disabled {
			t.Fatalf("unsupported export enabled: %q", hit.Label)
		}
	}
}
