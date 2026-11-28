package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestM12HeadingShortcutsAcrossModes(t *testing.T) {
	for _, mode := range []struct {
		name string
		id   gfx.ID
	}{{"source", editModeID}, {"split", splitModeID}, {"rendered", renderedModeID}} {
		t.Run(mode.name, func(t *testing.T) {
			state, driver := testDriver(t)
			current := state.session.Current()
			current.Replace("Body\n\nTail\n")
			clickID(t, driver, mode.id)
			if state.mode == renderedMode {
				clickID(t, driver, renderedRows.At(0))
			} else {
				clickID(t, driver, sourceID)
				current.Editor.MoveTo(2, false)
			}
			wantMode := state.mode
			for level := 1; level <= 7; level++ {
				key, prefix := '0'+rune(level), strings.Repeat("#", level)+" "
				if level == 7 {
					key, prefix = '0', ""
				}
				if _, err := driver.ShortcutChord(primaryModifier(), key); err != nil {
					t.Fatal(err)
				}
				if got := current.Text(); got != prefix+"Body\n\nTail\n" || state.mode != wantMode {
					t.Fatalf("shortcut %c: mode=%v source=%q", key, state.mode, got)
				}
			}
		})
	}
}

func TestM12ToolbarLayoutAndVisibility(t *testing.T) {
	t.Setenv("MARKSTACK_SETTINGS_PATH", filepath.Join(t.TempDir(), "settings.json"))
	state, driver := testDriver(t)
	for _, size := range [][2]int{{740, 500}, {1200, 740}, {1800, 1000}} {
		if err := driver.Resize(size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		var previous float32
		for _, group := range formatGroups {
			for _, tool := range group {
				hit := m12Hit(t, driver, tool.id)
				if hit.Bounds.Y < 64 || hit.Bounds.Y+hit.Bounds.Height > 98 || hit.Bounds.X < previous {
					t.Fatalf("%dx%d: toolbar overlap or unexpected focus policy: %+v", size[0], size[1], hit)
				}
				previous = hit.Bounds.X + hit.Bounds.Width
			}
		}
		zen := m12Hit(t, driver, formatZenID)
		if zen.Bounds.X < previous || zen.Bounds.X+zen.Bounds.Width > float32(size[0]) || state.sourceRect.Y < 98 {
			t.Fatalf("%dx%d: zen or source overlaps toolbar: %+v %+v", size[0], size[1], zen.Bounds, state.sourceRect)
		}
	}
	clickID(t, driver, viewMenuID)
	if !m12Hit(t, driver, viewMenuItems.At(5)).Checked {
		t.Fatal("toolbar should be checked by default")
	}
	clickID(t, driver, viewMenuItems.At(5))
	if !state.preferences.HideFormatBar || m3HasHit(driver, formatBoldID) || state.formatBarRect.Height != 0 {
		t.Fatal("hide toolbar did not collapse the formatting strip")
	}
	clickID(t, driver, viewMenuID)
	if m12Hit(t, driver, viewMenuItems.At(5)).Checked {
		t.Fatal("hidden toolbar should not be checked")
	}
	clickID(t, driver, viewMenuItems.At(5))
	clickID(t, driver, readModeID)
	if m3HasHit(driver, formatBoldID) {
		t.Fatal("read mode paints a formatting toolbar")
	}
	clickID(t, driver, editModeID)
	state.sidebar, state.outline = true, false
	clickID(t, driver, formatZenID)
	if state.sidebar || state.outline || !state.zen.active {
		t.Fatal("zen did not collapse both panels")
	}
	clickID(t, driver, formatZenID)
	if !state.sidebar || state.outline || state.zen.active {
		t.Fatal("zen did not restore the original panels")
	}
}

func TestM12ToolbarSelectionAndOneStepHistory(t *testing.T) {
	for _, rendered := range []bool{false, true} {
		name, mode, input := "source", editModeID, sourceID
		if rendered {
			name, mode, input = "rendered", renderedModeID, richInputID
		}
		t.Run(name, func(t *testing.T) {
			state, driver := testDriver(t)
			current := state.session.Current()
			current.Replace("你好 world\n")
			clickID(t, driver, mode)
			if rendered {
				clickID(t, driver, renderedRows.At(0))
			} else {
				clickID(t, driver, sourceID)
			}
			m12Select(t, driver, 0, 2)
			clickID(t, driver, formatBoldID)
			if got := current.Text(); got != "**你好** world\n" || driver.Snapshot().Focused != input {
				t.Fatalf("bold result %q focus=%v", got, driver.Snapshot().Focused)
			}
			clickID(t, driver, formatUndoID)
			if got := current.Text(); got != "你好 world\n" {
				t.Fatalf("one toolbar undo: %q", got)
			}
			clickID(t, driver, formatRedoID)
			if got := current.Text(); got != "**你好** world\n" {
				t.Fatalf("one toolbar redo: %q", got)
			}
			m12Chord(t, driver, primaryModifier(), '\\')
			if got := current.Text(); got != "你好 world\n" {
				t.Fatalf("clear formatting: %q", got)
			}
		})
	}
}

func TestM12TopMenusKeyboardPointerAndCheckmarks(t *testing.T) {
	t.Setenv("MARKSTACK_SETTINGS_PATH", filepath.Join(t.TempDir(), "settings.json"))
	state, driver := testDriver(t)
	current := state.session.Current()
	current.Replace("Body\n")
	clickID(t, driver, sourceID)
	m12Select(t, driver, 0, 4)
	clickID(t, driver, paragraphMenuID)
	if !m12Hit(t, driver, paragraphItems.At(0)).Checked || !m12Hit(t, driver, tableMenuItems.At(0)).Disabled {
		t.Fatal("paragraph menu does not reflect paragraph/table context")
	}
	m12Key(t, driver, gfx.KeyDown)
	m12Key(t, driver, gfx.KeyEnter)
	if current.Text() != "# Body\n" || driver.Snapshot().Focused != sourceID {
		t.Fatalf("keyboard heading changed %q focus=%v", current.Text(), driver.Snapshot().Focused)
	}
	clickID(t, driver, paragraphMenuID)
	if !m12Hit(t, driver, paragraphItems.At(1)).Checked {
		t.Fatal("active heading is not checked")
	}
	m12Key(t, driver, gfx.KeyEscape)
	clickID(t, driver, formatMenuID)
	m12Hit(t, driver, inlineFormatItems.At(0))
	if err := driver.Click(700, 470); err != nil {
		t.Fatal(err)
	}
	if !state.formatMenu.trigger.IsZero() {
		t.Fatal("outside click did not dismiss menu")
	}
	clickID(t, driver, viewMenuID)
	if !m12Hit(t, driver, viewMenuItems.At(2)).Checked {
		t.Fatal("source mode is not checked")
	}
	clickID(t, driver, viewMenuItems.At(0))
	if state.mode != renderedMode {
		t.Fatal("view menu did not change mode")
	}
	clickID(t, driver, themeMenuID)
	clickID(t, driver, themeMenuItems.At(1))
	if !state.dark || driver.NativeAppearance() != app.AppearanceDark {
		t.Fatal("theme menu did not switch the native appearance")
	}
	clickID(t, driver, themeMenuID)
	if !m12Hit(t, driver, themeMenuItems.At(1)).Checked {
		t.Fatal("dark appearance is not checked")
	}
	clickID(t, driver, themeMenuItems.At(7))
	clickID(t, driver, themeMenuID)
	if state.preferences.ReadingTheme != "nord" || !m12Hit(t, driver, themeMenuItems.At(7)).Checked {
		t.Fatal("reading theme did not persist with a checkmark")
	}
}

func TestM12RenderedBlockInsertFocusAndHistory(t *testing.T) {
	for _, fixture := range []struct {
		name, contains string
		insert         func(*testing.T, *app.Driver)
		focus          gfx.ID
	}{
		{"fence", "```\r\n正文\r\n```", func(t *testing.T, driver *app.Driver) { m12Chord(t, driver, primaryModifier()|gfx.ModifierAlt, 'c') }, codeInputID},
		{"math", "$$\r\n\r\n$$", func(t *testing.T, driver *app.Driver) { m12Chord(t, driver, primaryModifier()|gfx.ModifierAlt, 'b') }, codeInputID},
		{"table 6x6", "| 列 1 | 列 2 | 列 3 | 列 4 | 列 5 | 列 6 |", func(t *testing.T, driver *app.Driver) {
			clickID(t, driver, formatTableTriggerID)
			clickID(t, driver, tableGridCells.At(35))
		}, tableInputID},
		{"mermaid", "```mermaid\r\nsequenceDiagram", func(t *testing.T, driver *app.Driver) {
			clickID(t, driver, formatDiagramTriggerID)
			clickID(t, driver, diagramItems.At(1))
		}, codeInputID},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.preferences.Language = "zh-CN"
			path := filepath.Join(t.TempDir(), "structural.md")
			before := "# Header\r\n\r\n正文\r\n\r\nTail\r\n"
			if err := os.WriteFile(path, []byte("\ufeff"+before), 0600); err != nil {
				t.Fatal(err)
			}
			state.openFile(path)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(1))
			fixture.insert(t, driver)
			after := state.session.Current().Text()
			if state.mode != renderedMode || driver.Snapshot().Focused != fixture.focus || !strings.Contains(after, fixture.contains) || !strings.HasSuffix(after, "\r\n\r\nTail\r\n") {
				t.Fatalf("insert %q: mode=%v focus=%v code=%v table=%v", after, state.mode, driver.Snapshot().Focused, state.codeBlock.active, state.table.active)
			}
			if fixture.focus == codeInputID && (!state.codeBlock.active || state.codeBlock.input.Caret() != 0) || fixture.focus == tableInputID && (!state.table.active || state.table.row != 0 || state.table.column != 0) {
				t.Fatal("insert did not activate the first code line or table cell")
			}
			m12Chord(t, driver, primaryModifier(), 'z')
			if state.session.Current().Text() != before || state.mode != renderedMode || driver.Snapshot().Focused != richInputID {
				t.Fatalf("one undo: mode=%v source=%q", state.mode, state.session.Current().Text())
			}
			m12Chord(t, driver, primaryModifier()|gfx.ModifierShift, 'z')
			if state.session.Current().Text() != after || state.mode != renderedMode || driver.Snapshot().Focused != fixture.focus {
				t.Fatalf("one redo: mode=%v source=%q focus=%v", state.mode, state.session.Current().Text(), driver.Snapshot().Focused)
			}
			m12Chord(t, driver, primaryModifier(), 's')
			if saved, err := os.ReadFile(path); err != nil || string(saved) != "\ufeff"+after {
				t.Fatalf("save changed BOM or bytes: %q (%v)", saved, err)
			}
		})
	}
}

func TestM12CalloutFrontMatterTOCAndFootnote(t *testing.T) {
	for _, mode := range []gfx.ID{renderedModeID, splitModeID} {
		state, driver := testDriver(t)
		current := state.session.Current()
		current.Replace("Body\n\nTail\n")
		clickID(t, driver, mode)
		wantMode := state.mode
		if wantMode == renderedMode {
			clickID(t, driver, renderedRows.At(0))
		} else {
			clickID(t, driver, sourceID)
			current.Editor.MoveTo(1, false)
		}
		clickID(t, driver, formatCalloutTriggerID)
		clickID(t, driver, calloutItems.At(0))
		if !strings.Contains(current.Text(), "> [!NOTE]\n> ") || state.mode != wantMode {
			t.Fatalf("callout: %q mode=%v", current.Text(), state.mode)
		}
		clickID(t, driver, formatTOCID)
		if !strings.Contains(current.Text(), "[TOC]") || state.mode != wantMode {
			t.Fatalf("TOC: %q mode=%v", current.Text(), state.mode)
		}
		state.runFormat(cmdFrontMatter)
		if !strings.HasPrefix(current.Text(), "---\ntitle: \ndate: \n---\n\n") || state.mode != wantMode {
			t.Fatalf("front matter: %q mode=%v", current.Text(), state.mode)
		}
		if wantMode == renderedMode {
			state.activateRendered(m12BodyIndex(t, state, "Body"))
		} else {
			current.Editor.MoveTo(strings.Index(current.Text(), "Body")+4, false)
		}
		state.runFormat(cmdFootnote)
		if !strings.Contains(current.Text(), "[^1]") || !strings.Contains(current.Text(), "\n[^1]: ") || state.mode != wantMode {
			t.Fatalf("footnote: %q mode=%v", current.Text(), state.mode)
		}
	}
}

func TestM12TableCommandsInSourceAndRendered(t *testing.T) {
	for _, mode := range []gfx.ID{editModeID, renderedModeID} {
		state, driver := testDriver(t)
		current := state.session.Current()
		before := "|A|B|\n|---|:---:|\n|one|two|\n\nTail\n"
		current.Replace(before)
		clickID(t, driver, mode)
		wantMode := state.mode
		if wantMode == renderedMode {
			clickID(t, driver, renderedTableCells.At(3))
		} else {
			clickID(t, driver, sourceID)
			current.Editor.MoveTo(strings.Index(before, "two"), false)
		}
		clickID(t, driver, paragraphMenuID)
		if m12Hit(t, driver, tableMenuItems.At(1)).Disabled {
			t.Fatal("table command is disabled in a table")
		}
		clickID(t, driver, tableMenuItems.At(1))
		if !strings.Contains(current.Text(), "|one|two|\n|   |   |\n\nTail\n") || state.mode != wantMode {
			t.Fatalf("insert table row: mode=%v source=%q", state.mode, current.Text())
		}
		m12Chord(t, driver, primaryModifier(), 'z')
		clickID(t, driver, paragraphMenuID)
		clickID(t, driver, tableMenuItems.At(10))
		if current.Text() != "| A | B |\n| --- | :---: |\n| one | two |\n\nTail\n" || state.mode != wantMode {
			t.Fatalf("format table: mode=%v source=%q", state.mode, current.Text())
		}
		m12Chord(t, driver, primaryModifier(), 'z')
		if current.Text() != before || state.mode != wantMode {
			t.Fatalf("undo table format: mode=%v source=%q", state.mode, current.Text())
		}
	}
}

func TestM12SourceShortcutsAndMenuRespectIME(t *testing.T) {
	state, driver := testDriver(t)
	current := state.session.Current()
	current.Replace("Body\n")
	clickID(t, driver, sourceID)
	m12Select(t, driver, 0, 4)
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	before := app.TextStateOf(current.Editor)
	for _, trigger := range []gfx.ID{formatBoldID, formatCodeBlockID, paragraphMenuID, formatMenuID} {
		clickID(t, driver, trigger)
		requireNativeComposition(t, driver, sourceID, before, "source toolbar and menus")
	}
	m12Chord(t, driver, primaryModifier(), '3')
	requireNativeComposition(t, driver, sourceID, before, "heading shortcut")
	if current.Editor.CommittedString() != "Body\n" || !state.formatMenu.trigger.IsZero() {
		t.Fatal("formatting committed preedit or opened a menu during composition")
	}
	current.Editor.CancelComposition()
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	m12Select(t, driver, 0, 4)
	m12Chord(t, driver, primaryModifier(), 'u')
	if current.Text() != "<u>Body</u>\n" || driver.Snapshot().Focused != sourceID {
		t.Fatalf("underline after cancel: %q", current.Text())
	}
	m12Chord(t, driver, primaryModifier(), '\\')
	if current.Text() != "Body\n" {
		t.Fatalf("clear underline: %q", current.Text())
	}
	m12Select(t, driver, 0, 4)
	m12Chord(t, driver, primaryModifier(), 'k')
	if !state.linkOpen || driver.Snapshot().Focused != linkInputID || state.linkRect.Y < 98 {
		t.Fatalf("source link did not open below the toolbar: %+v", state.linkRect)
	}
	if err := driver.Type("https://example.invalid"); err != nil {
		t.Fatal(err)
	}
	m12Key(t, driver, gfx.KeyEnter)
	if current.Text() != "[Body](https://example.invalid)\n" || driver.Snapshot().Focused != sourceID {
		t.Fatalf("source link: %q focus=%v", current.Text(), driver.Snapshot().Focused)
	}
}

func TestM12LongMenuKeyboardAndNewParagraphTyping(t *testing.T) {
	for _, option := range []struct {
		stepsUp int
		want    string
	}{{5, "New\n\nBody\n\nTail\n"}, {4, "Body\n\nNew\n\nTail\n"}} {
		state, driver := testDriver(t)
		current := state.session.Current()
		current.Replace("Body\n\nTail\n")
		if err := driver.Resize(740, 500); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, renderedModeID)
		clickID(t, driver, renderedRows.At(0))
		clickID(t, driver, paragraphMenuID)
		m12Key(t, driver, gfx.KeyEnd)
		for range option.stepsUp {
			m12Key(t, driver, gfx.KeyUp)
		}
		m12Key(t, driver, gfx.KeyEnter)
		if state.mode != renderedMode || driver.Snapshot().Focused != richInputID {
			t.Fatalf("new paragraph is not focused: %q", current.Text())
		}
		if err := driver.Type("New"); err != nil {
			t.Fatal(err)
		}
		if current.Text() != option.want || state.mode != renderedMode {
			t.Fatalf("type in new paragraph: %q, want %q", current.Text(), option.want)
		}
	}
}

func TestM12ClearFormattingKeepsInlineCodeContentLiteral(t *testing.T) {
	for _, sample := range []struct{ source, want string }{
		{"**`a*b`** and `[x](y)`", "a*b and [x](y)"},
		{"<u>**[文字](https://example.invalid)**</u> ==词== ~~完成~~ <sub>2</sub> <sup>3</sup>", "文字 词 完成 2 3"},
		{"\\*escaped\\* ![image](local.png)", "\\*escaped\\* ![image](local.png)"},
	} {
		if got := clearFormatText(sample.source); got != sample.want {
			t.Errorf("clear %q: %q, want %q", sample.source, got, sample.want)
		}
	}
}

func m12BodyIndex(t *testing.T, state *model, value string) int {
	t.Helper()
	for index := range state.parsed.Blocks {
		for _, span := range state.parsed.Spans(&state.parsed.Blocks[index]) {
			if span.Text == value {
				return index
			}
		}
	}
	t.Fatalf("could not find rendered block %q", value)
	return -1
}

func m12Hit(t *testing.T, driver *app.Driver, id gfx.ID) gfx.Hit {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("control %v is not visible", id)
	return gfx.Hit{}
}

func m12Chord(t *testing.T, driver *app.Driver, modifiers gfx.Modifiers, key rune) {
	t.Helper()
	if _, err := driver.ShortcutChord(modifiers, key); err != nil {
		t.Fatal(err)
	}
}

func m12Key(t *testing.T, driver *app.Driver, key gfx.Key) {
	t.Helper()
	if err := driver.Key(key, 0); err != nil {
		t.Fatal(err)
	}
}

func m12Select(t *testing.T, driver *app.Driver, start, end int) {
	t.Helper()
	if err := driver.SelectText(start, end); err != nil {
		t.Fatal(err)
	}
}
