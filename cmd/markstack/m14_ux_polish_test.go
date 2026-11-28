package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestM14AiryChrome(t *testing.T) {
	state, driver := testDriver(t)
	if got := newConfig(state).TitlebarHeight; got != 38 {
		t.Errorf("titlebar height = %v, want 38", got)
	}
	modes := []gfx.ID{renderedModeID, splitModeID, editModeID, readModeID}
	for _, size := range [][2]int{{740, 500}, {1200, 780}, {1800, 1000}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			if err := driver.Resize(size[0], size[1]); err != nil {
				t.Fatal(err)
			}
			newTab := m12Hit(t, driver, newFileID).Bounds
			tab := m12Hit(t, driver, tabClose.At(0)).Bounds
			bar := state.formatBarRect
			if newTab.Y < 38 || newTab.Y+newTab.Height > 72 || newTab.X < tab.X+tab.Width || newTab.X-(tab.X+tab.Width) > 18 || bar.Y != 72 || bar.Height != 38 || state.sourceRect.Y < 110 {
				t.Errorf("adjacent chrome: tab=%+v new=%+v format=%+v source=%+v", tab, newTab, bar, state.sourceRect)
			}
			for _, id := range []gfx.ID{topSaveID, topSaveAsID} {
				if m3HasHit(driver, id) {
					t.Errorf("obsolete topbar control %v is still visible", id)
				}
			}
			var right float32
			for _, id := range modes {
				hit := m12Hit(t, driver, id)
				if hit.Hint == "" || hit.Bounds.Width != 34 || hit.Bounds.X < right || hit.Bounds.Y+hit.Bounds.Height > 38 {
					t.Errorf("mode hit = %+v", hit)
				}
				right = hit.Bounds.X + hit.Bounds.Width
				for _, label := range driver.Snapshot().Labels {
					if label.Bounds.Y < 38 && label.Bounds.X >= hit.Bounds.X && label.Bounds.X < right {
						t.Errorf("mode button paints text %q", label.Value)
					}
				}
			}
			autoSave := m12Hit(t, driver, topAutoSaveID).Bounds
			if autoSave.Y < float32(size[1]-26) || autoSave.X >= float32(size[0])/2 {
				t.Errorf("auto-save is not in the bottom-left status bar: %+v", autoSave)
			}
		})
	}
	clickID(t, driver, topAutoSaveID)
	if !state.preferences.AutoSave {
		t.Fatal("status switch did not enable auto-save")
	}
	clickID(t, driver, topAutoSaveID)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, newFileID)
	if state.preferences.AutoSave || len(state.session.Tabs) != 2 || state.mode != renderedMode || driver.Snapshot().Focused != richInputID {
		t.Fatalf("new tab: mode=%v focus=%v tabs=%d auto-save=%v", state.mode, driver.Snapshot().Focused, len(state.session.Tabs), state.preferences.AutoSave)
	}
}

func TestM14FenceTriggers(t *testing.T) {
	for _, marker := range []struct{ typed, canonical string }{
		{"```", "```"}, {"```go", "```go"}, {"```  ", "```"}, {"、、、", "```"}, {"···python", "```python"}, {"｀｀｀go \t", "```go"},
	} {
		for _, area := range []struct{ name, source, prefix, suffix string }{
			{"empty", "", "", ""}, {"empty-line", "\n", "", "\n"},
			{"trailing", "Head", "Head\n\n", ""},
			{"before-paragraph", "Head\n\nTail", "Head\n\n", "\n\nTail"},
			{"before-heading", "Head\n\n# Tail", "Head\n\n", "\n\n# Tail"},
			{"before-code", "Head\n\n```go\nTail\n```", "Head\n\n", "\n\n```go\nTail\n```"},
			{"before-list", "Head\n\n- Tail", "Head\n\n", "\n\n- Tail"},
		} {
			t.Run(marker.typed+"/"+area.name, func(t *testing.T) {
				state, driver := testDriver(t)
				if area.source != "" {
					if err := state.session.Current().EditRange(0, 0, area.source); err != nil {
						t.Fatal(err)
					}
					state.session.Current().Editor.MoveTo(0, false)
				}
				clickID(t, driver, renderedModeID)
				if area.prefix != "" {
					clickID(t, driver, renderedRows.At(0))
					m12Key(t, driver, gfx.KeyEnd)
					m12Key(t, driver, gfx.KeyEnter)
				}
				for _, character := range marker.typed {
					if err := driver.Type(string(character)); err != nil {
						t.Fatal(err)
					}
					if state.mode != renderedMode || !state.rich.active || state.codeBlock.active {
						t.Fatalf("typing %q left the paragraph: mode=%v source=%q", character, state.mode, state.session.Current().Text())
					}
				}
				m12Key(t, driver, gfx.KeyEnter)
				want := area.prefix + marker.canonical + "\n\n```" + area.suffix
				if state.mode != renderedMode || !state.codeBlock.active || driver.Snapshot().Focused != codeInputID || state.codeBlock.input.Caret() != 0 || state.session.Current().Text() != want {
					t.Fatalf("fence: source=%q want=%q mode=%v active=%v focus=%v message=%q", state.session.Current().Text(), want, state.mode, state.codeBlock.active, driver.Snapshot().Focused, state.message)
				}
			})
		}
	}
}

func TestM14FenceInExistingParagraphPreservesBOMAndCRLF(t *testing.T) {
	for _, marker := range []string{"···go \t", "｀｀｀go \t"} {
		t.Run(marker, func(t *testing.T) {
			state, driver, path, _ := renderedSourceCollisionFixture(t, "# Head\n\n"+marker+"\n\n- Tail\n", false, 1200)
			clickID(t, driver, renderedRows.At(1))
			m12Key(t, driver, gfx.KeyEnd)
			if !state.rich.active || state.rich.virtual {
				t.Fatal("fixture did not activate an existing paragraph")
			}
			m12Key(t, driver, gfx.KeyEnter)
			current := state.session.Current()
			want := "# Head\r\n\r\n```go\r\n\r\n```\r\n\r\n- Tail\r\n"
			if got := current.Text(); got != want || state.mode != renderedMode || !state.codeBlock.active || driver.Snapshot().Focused != codeInputID {
				t.Fatalf("fence: source=%q want=%q mode=%v active=%v focus=%v", got, want, state.mode, state.codeBlock.active, driver.Snapshot().Focused)
			}
			expectRenderedCollisionSave(t, state, driver, path, []byte("\xef\xbb\xbf"+want))
			m1Shortcut(t, driver, primaryModifier(), 'z')
			if !state.rich.active || current.Text() != "# Head\r\n\r\n"+marker+"\r\n\r\n- Tail\r\n" {
				t.Fatalf("undo did not restore the original text: %q", current.Text())
			}
		})
	}
}

func TestM14ClickBelowDocument(t *testing.T) {
	for _, scenario := range []struct{ name, source, want string }{
		{"empty", "", "next"}, {"paragraph", "Body", "Bodynext"},
		{"heading", "# Heading", "# Heading\n\nnext"},
		{"code", "```go\ncode\n```\n", "```go\ncode\n```\n\nnext"},
		{"table", "| A | B |\n| --- | --- |\n| a | b |", "| A | B |\n| --- | --- |\n| a | b |\n\nnext"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state, driver := testDriver(t)
			if err := state.session.Current().EditRange(0, 0, scenario.source); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			bounds := state.previewRect
			trailing := m12Hit(t, driver, renderedTrailingID)
			if trailing.Bounds.X != bounds.X || trailing.Bounds.Width != bounds.Width || trailing.Bounds.Y+trailing.Bounds.Height != bounds.Y+bounds.Height || trailing.Focusable {
				t.Fatalf("unexpected canvas hit: %+v", trailing)
			}
			if err := driver.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height-20); err != nil {
				t.Fatal(err)
			}
			if driver.Snapshot().Focused != richInputID || !state.rich.active || state.rich.input.Caret() != state.rich.input.RuneCount() {
				t.Fatalf("document end did not focus paragraph end: %v, active=%v", driver.Snapshot().Focused, state.rich.active)
			}
			if err := driver.Type("next"); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != scenario.want || state.mode != renderedMode {
				t.Fatalf("typed at end: %q, want %q (mode %v)", got, scenario.want, state.mode)
			}
		})
	}
}

func TestM14CodePlaceholder(t *testing.T) {
	if chromeTooltipOptions.MaxWidth < 320 {
		t.Errorf("tooltip max width = %v, want at least 320", chromeTooltipOptions.MaxWidth)
	}
	state := newModel()
	for language, want := range map[string]string{"zh": "输入代码…", "en": "Type code…"} {
		state.preferences.Language = language
		if got := state.codePlaceholder(); strings.TrimSpace(got) != want {
			t.Errorf("%s placeholder = %q, want %q", language, got, want)
		}
	}
}
