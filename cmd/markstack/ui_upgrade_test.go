package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func upgradeKey(t *testing.T, driver *app.Driver, key gfx.Key, mods gfx.Modifiers) {
	t.Helper()
	if err := driver.Key(key, mods); err != nil {
		t.Fatal(err)
	}
}

func upgradeShortcut(t *testing.T, driver *app.Driver, mods gfx.Modifiers, key rune) {
	t.Helper()
	if handled, err := driver.ShortcutChord(mods, key); err != nil || !handled {
		t.Fatalf("shortcut %q: handled=%v err=%v", key, handled, err)
	}
}

func TestCommandPaletteKeyboard(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sourceID)
	upgradeShortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'P')
	if !hasText(driver, state.local("命令面板", "Command Palette")) {
		t.Fatal("command palette not visible")
	}
	for _, character := range []string{"t", "g", "l"} {
		if err := driver.KeyDown(gfx.KeyUnknown, character); err != nil {
			t.Fatal(err)
		}
		if err := driver.KeyUp(gfx.KeyUnknown, character); err != nil {
			t.Fatal(err)
		}
	}
	if state.palette.query.String() != "tgl" {
		t.Fatalf("physical keys did not reach the palette input: %q", state.palette.query.String())
	}
	if hasText(driver, state.local("新建文档", "New Document")) {
		t.Fatal("unmatched command still visible")
	}
	upgradeKey(t, driver, gfx.KeyDown, 0)
	if state.palette.selected != 1 {
		t.Fatal("Down did not select the next result")
	}
	upgradeKey(t, driver, gfx.KeyUp, 0)
	if state.palette.selected != 0 {
		t.Fatal("Up did not restore the first result")
	}
	upgradeKey(t, driver, gfx.KeyEscape, 0)
	if hasText(driver, state.local("命令面板", "Command Palette")) {
		t.Fatal("Escape did not close palette")
	}
	if err := driver.Type("draft"); err != nil || state.session.Current().Text() != "draft" {
		t.Fatalf("Escape did not restore source focus: %v", err)
	}
	upgradeShortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'p')
	if err := driver.Type("nwdc"); err != nil {
		t.Fatal(err)
	}
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	if len(state.session.Tabs) != 2 {
		t.Fatal("Enter did not execute the fuzzy-matched command")
	}
}

func TestQuickFilePaletteAndEmptySidebar(t *testing.T) {
	state, driver := testDriver(t)
	root := t.TempDir()
	for _, name := range []string{"Other.md", "Release Notes.md", "service.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("sample"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == state.local("打开文件夹", "Open Folder") {
			clickID(t, driver, hit.ID)
			break
		}
	}
	if state.session.Root == "" {
		t.Fatal("empty sidebar CTA failed to open a workspace")
	}
	for _, match := range []struct{ query, name string }{{"rlnts", "Release Notes.md"}, {"srvg", "service.go"}, {"rlnts", "Release Notes.md"}} {
		upgradeShortcut(t, driver, primaryModifier(), 'p')
		if err := driver.Type(match.query); err != nil {
			t.Fatal(err)
		}
		if !hasText(driver, match.name) {
			t.Fatalf("missing fuzzy result %q", match.name)
		}
		if len(state.palette.results) != 1 {
			t.Fatalf("expected one result, got %d for %q", len(state.palette.results), match.query)
		}
		upgradeKey(t, driver, gfx.KeyEnter, 0)
		if got := filepath.Base(state.session.Current().Path); got != match.name {
			t.Fatalf("opened %q, want %q", got, match.name)
		}
	}
}

func TestCommandPaletteScrollsAndDoesNotCoverQuitConfirmation(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Unsaved changes")
	upgradeShortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'p')
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == paletteRows.At(0) {
			if err := driver.Scroll(gfx.ScrollEvent{X: hit.Bounds.X + 12, Y: hit.Bounds.Y + 12, DeltaY: 40}); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if upgradeHasHit(driver, paletteRows.At(0)) || !upgradeHasHit(driver, paletteRows.At(8)) {
		t.Fatal("scroll did not reveal the next result")
	}
	state.requestClose(func() { t.Error("quit ignored an unsaved document") })
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if upgradeHasHit(driver, paletteInputID) || !upgradeHasHit(driver, cancelConfirmID) {
		t.Fatal("command palette still covers the unsaved document confirmation")
	}
	clickID(t, driver, cancelConfirmID)
	if state.session.Pending() != nil || state.session.Current().Text() != "Unsaved changes" {
		t.Fatal("cancel quit did not keep the draft")
	}
}

func TestPaletteAndFormatBubbleRespectSourceComposition(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sourceID)
	if err := driver.Type("note"); err != nil {
		t.Fatal(err)
	}
	if err := driver.SelectText(0, 4); err != nil {
		t.Fatal(err)
	}
	if !upgradeHasHit(driver, selectionFormatButtons.At(0)) {
		t.Fatal("selection bubble not visible before composition")
	}
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	upgradeShortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'p')
	if state.palette.query != nil || upgradeHasHit(driver, selectionFormatButtons.At(0)) || !state.session.Current().Composing() {
		t.Fatal("palette or formatting bubble interrupted marked text")
	}
}

func TestEmptySidebarKeepsRenderedComposition(t *testing.T) {
	state, driver := testDriver(t)
	root := t.TempDir()
	path := filepath.Join(root, "draft.md")
	if err := os.WriteFile(path, []byte("Existing document\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	current := state.session.Current()
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, emptySidebarOpenID)
	if state.session.Current() != current || state.session.Root != "" || state.session.Pending() != nil || !state.richComposing() {
		t.Fatal("opening a workspace interrupted rendered marked text")
	}
}

func TestSelectionFormatBubbleCreatesLink(t *testing.T) {
	for _, mode := range []viewMode{editMode, renderedMode} {
		state, driver := testDriver(t)
		current := state.session.Current()
		current.Replace("one 中文 three\r\n")
		if mode == renderedMode {
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
		} else {
			clickID(t, driver, sourceID)
		}
		if err := driver.SelectText(4, 6); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, selectionFormatButtons.At(3))
		if err := driver.Type("https://example.com"); err != nil {
			t.Fatal(err)
		}
		upgradeKey(t, driver, gfx.KeyEnter, 0)
		if got, want := current.Text(), "one [中文](https://example.com) three\r\n"; got != want {
			t.Fatalf("mode %v: got %q; want %q", mode, got, want)
		}
	}
}

func TestReadCodeHeaderCopiesAndShowsToast(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "en"
	state.session.Current().Replace("```sql\nSELECT 1;\n```\n")
	clickID(t, driver, readModeID)
	if !upgradeExactLabel(driver, "SQL") || !upgradeExactLabel(driver, "Copy") {
		t.Fatal("reading view did not paint the code header")
	}
	clickID(t, driver, codeCopyButtons.At(0))
	if driver.Clipboard() != "SELECT 1;" || state.message != "已复制代码" || !upgradeExactLabel(driver, "Copied code block") {
		t.Fatalf("copy result: clipboard=%q status=%q", driver.Clipboard(), state.message)
	}
}

func upgradeHasHit(driver *app.Driver, id gfx.ID) bool {
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			return true
		}
	}
	return false
}

func TestSaveToastExpiresWithoutChangingStatus(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.QueueSaveFile(filepath.Join(t.TempDir(), "toast.md"), nil); err != nil {
		t.Fatal(err)
	}
	upgradeShortcut(t, driver, primaryModifier(), 's')
	status := state.message
	caption := state.local("已保存", "Saved")
	if !upgradeExactLabel(driver, caption) || status != caption+" toast.md" {
		t.Fatalf("missing toast or changed status: %q", status)
	}
	if err := driver.Advance(2500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if upgradeExactLabel(driver, caption) || state.message != status {
		t.Fatal("toast did not expire independently of status")
	}
}

func upgradeExactLabel(driver *app.Driver, value string) bool {
	for _, label := range driver.Snapshot().Labels {
		if label.Value == value {
			return true
		}
	}
	return false
}

func TestSelectionFormatBubble(t *testing.T) {
	for _, format := range []struct{ zh, en, marker string }{{"加粗", "Bold", "**"}, {"斜体", "Italic", "*"}, {"行内代码", "Inline Code", "`"}} {
		for _, mode := range []string{"source", "rendered"} {
			t.Run(format.en+"/"+mode, func(t *testing.T) {
				state, driver := testDriver(t)
				current := state.session.Current()
				current.Replace("one 中文 three\r\n")
				if mode == "rendered" {
					clickID(t, driver, renderedModeID)
					clickID(t, driver, renderedRows.At(0))
					state.rich.input.SetSelection(text.Range{Start: 4, End: 6})
				} else {
					clickID(t, driver, sourceID)
					current.Editor.SetSelection(text.Range{Start: 4, End: 6})
				}
				if err := driver.Resize(1200, 780); err != nil {
					t.Fatal(err)
				}
				for _, hit := range driver.Snapshot().Hits {
					if hit.Label == state.local(format.zh, format.en) {
						clickID(t, driver, hit.ID)
						break
					}
				}
				if want := "one " + format.marker + "中文" + format.marker + " three\r\n"; current.Text() != want {
					t.Fatalf("formatted source = %q, want %q", current.Text(), want)
				}
			})
		}
	}
}
