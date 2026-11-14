package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

func TestWindowUsesCompactCustomTitlebar(t *testing.T) {
	config := newConfig(newModel())
	if config.WindowMode != app.WindowCustomTitlebar || config.TitlebarHeight != 32 {
		t.Fatalf("window chrome = %v, height %v", config.WindowMode, config.TitlebarHeight)
	}
}

func TestCompactTitlebarKeepsActionsAndDragging(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	snapshot := driver.Snapshot()
	for _, id := range []gfx.ID{fileMenuID, newFileID, editModeID, splitModeID, readModeID} {
		found := false
		for _, hit := range snapshot.Hits {
			if hit.ID == id && hit.Bounds.X >= 70 && hit.Bounds.X+hit.Bounds.Width <= 740 && hit.Bounds.Y+hit.Bounds.Height <= 32 {
				found = true
			}
		}
		if !found {
			t.Errorf("titlebar action %v is obscured or outside the 32px strip", id)
		}
	}
	moveRegion := false
	for _, hit := range snapshot.Hits {
		if hit.MovesWindow && hit.Bounds.Y <= 0 && hit.Bounds.Height >= 32 {
			moveRegion = true
		}
	}
	if !moveRegion {
		t.Fatal("custom titlebar has no drag region")
	}
	clickID(t, driver, newFileID)
	if len(state.session.Tabs) != 2 {
		t.Fatal("titlebar New action did not create one document")
	}
}

func TestTitlebarPressRoutesMovementWithoutStealingCommands(t *testing.T) {
	for _, width := range []int{740, 1200, 1605} {
		_, driver := testDriver(t)
		if err := driver.Resize(width, 955); err != nil {
			t.Fatal(err)
		}
		left, right := float32(0), float32(0)
		var menu ui.Rect
		for _, hit := range driver.Snapshot().Hits {
			switch hit.ID {
			case findDocumentID:
				left = hit.Bounds.X + hit.Bounds.Width
			case renderedModeID:
				right = hit.Bounds.X
			case fileMenuID:
				menu = hit.Bounds
			}
		}
		if left <= 0 || right <= left || menu.Width <= 0 {
			t.Fatalf("width %d: missing titlebar geometry: left=%v right=%v menu=%+v", width, left, right, menu)
		}
		middle := (left + right) / 2
		if got := driver.PressAt(middle, 16); got != app.PressMove {
			t.Errorf("width %d: blank titlebar press=%v, want move", width, got)
		}
		if got := driver.PressAt(menu.X+menu.Width/2, menu.Y+menu.Height/2); got != app.PressControl {
			t.Errorf("width %d: file menu press=%v, want control", width, got)
		}
		if got := driver.PressAt(middle, 60); got == app.PressMove || got == app.PressMoveOnDrag {
			t.Errorf("width %d: document area incorrectly moves the window", width)
		}
		clickID(t, driver, fileMenuID)
		if got := driver.PressAt(middle, 16); got != app.PressMoveOnDrag {
			t.Errorf("width %d: menu backdrop press=%v, want move on drag", width, got)
		}
	}
}

func TestFileMenuExposesEveryCommandAtMinimumWidth(t *testing.T) {
	_, driver := testDriver(t)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	for _, id := range []gfx.ID{openFolderID, openFileID, saveID, saveAsID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id && hit.Bounds.X >= 0 && hit.Bounds.X+hit.Bounds.Width <= 740 {
				found = true
			}
		}
		if !found {
			t.Errorf("file menu command %v is not reachable", id)
		}
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == saveAsID {
			t.Fatal("Escape did not close file menu")
		}
	}
}

func TestReadModeUsesFullHeightWithoutEditorStatusBar(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Reading")
	clickID(t, driver, readModeID)
	snapshot := driver.Snapshot()
	for _, primitive := range snapshot.Primitives {
		if primitive.Bounds.X == 0 && primitive.Bounds.Y == 754 && primitive.Bounds.Width == 1200 && primitive.Bounds.Height == 1 {
			t.Fatal("read mode still paints the editor status bar separator")
		}
	}
	if state.previewRect.Y+state.previewRect.Height != 780 {
		t.Fatalf("read mode content ends at %v, want window bottom 780", state.previewRect.Y+state.previewRect.Height)
	}
}

func TestThemeToggleUpdatesNativeAppearance(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, themeToggleID)
	if !state.dark || driver.NativeAppearance() != app.AppearanceDark {
		t.Fatalf("dark theme=%v native appearance=%v", state.dark, driver.NativeAppearance())
	}
	clickID(t, driver, themeToggleID)
	if state.dark || driver.NativeAppearance() != app.AppearanceLight {
		t.Fatalf("light theme=%v native appearance=%v", state.dark, driver.NativeAppearance())
	}
}

func TestTyporaViewportReadingAnchors(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# 墨栈\n\n一处整理，一处写作。")
	if err := driver.Resize(1605, 955); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	if state.treeRect.Width < 255 || state.treeRect.Width > 257 {
		t.Fatalf("sidebar content width=%v", state.treeRect.Width)
	}
	for _, text := range driver.Snapshot().Texts {
		if text.Value == "墨栈" && text.Style.Size >= 31 {
			if text.Bounds.X < 455 || text.Bounds.X > 460 || text.Bounds.Y < 108 || text.Bounds.Y > 112 {
				t.Fatalf("reading heading bounds = %+v", text.Bounds)
			}
			return
		}
	}
	t.Fatal("reading heading not rendered")
}

func TestDarkThemeUsesCobaltSemanticPalette(t *testing.T) {
	state := newModel()
	state.dark = true
	theme, palette := state.look()
	if palette.background != ui.RGB(32, 37, 46) || palette.sidebar != ui.RGB(27, 32, 40) || palette.bar != ui.RGB(38, 44, 54) {
		t.Fatalf("dark surfaces = %+v", palette)
	}
	if theme.Foreground != palette.ink || theme.PopoverForeground != palette.ink || theme.Popover != palette.bar || theme.AccentForeground != palette.accent {
		t.Fatalf("dark foreground = %+v", theme.Foreground)
	}
}

func focusedControl(t *testing.T, driver *app.Driver) (gfx.Hit, bool) {
	t.Helper()
	snapshot := driver.Snapshot()
	for _, hit := range snapshot.Hits {
		if hit.ID == snapshot.Focused {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// assertAccessibleControlNames checks the part of VoiceOver support that can be
// verified from the paint tree: every enabled focusable control carries a
// non-empty accessible name and a role, and disabled controls expose their
// disabled state rather than disappearing.
func assertAccessibleControlNames(t *testing.T, driver *app.Driver, stage string) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if !hit.Focusable || hit.Disabled || hit.IsLabel {
			continue
		}
		if hit.Label == "" {
			t.Errorf("%s: focusable control %v has no accessible name: %+v", stage, hit.ID, hit)
		}
		if hit.Role == 0 {
			t.Errorf("%s: focusable control %q has no role", stage, hit.Label)
		}
	}
}

// TestChromeKeyboardTabOrderSkipsDisabledControlsAndNamesEveryControl walks the
// keyboard focus order only through Tab: disabled mode controls are skipped,
// each stop has an accessible name, the titlebar order follows the visual
// order, and Shift+Tab reverses it.
func TestChromeKeyboardTabOrderSkipsDisabledControlsAndNamesEveryControl(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	state.session.Current().Kind = workspace.RichHTMLKind
	state.session.Current().Replace("<p>正文</p>")
	var order []gfx.ID
	start := driver.Snapshot().Focused
	for range 40 {
		hit, ok := focusedControl(t, driver)
		if !ok {
			break
		}
		if hit.Disabled {
			t.Fatalf("Tab focused disabled control %q", hit.Label)
		}
		if hit.Label == "" || hit.Role == 0 {
			t.Fatalf("Tab focused unnamed control %v: %+v", hit.ID, hit)
		}
		order = append(order, hit.ID)
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		if next := driver.Snapshot().Focused; next == start {
			break
		}
	}
	visited := map[gfx.ID]bool{}
	for _, id := range order {
		visited[id] = true
	}
	for _, id := range []gfx.ID{sourceID, sidebarToggleID, fileMenuID, newFileID, findDocumentID, renderedModeID, editModeID, readModeID, outlineToggleID, themeToggleID, workspaceOpenID, refreshID, searchID} {
		if !visited[id] {
			t.Errorf("Tab order never reached %v (%v stops)", id, len(order))
		}
	}
	if visited[splitModeID] {
		t.Error("disabled split mode was not skipped")
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == splitModeID && !hit.Disabled {
			t.Error("split mode is not exposed as disabled for rich text")
		}
	}
	titlebarOrder := []gfx.ID{sidebarToggleID, fileMenuID, newFileID, findDocumentID, renderedModeID, editModeID, readModeID, outlineToggleID, themeToggleID}
	next := 0
	for _, id := range order {
		if next < len(titlebarOrder) && id == titlebarOrder[next] {
			next++
		}
	}
	if next != len(titlebarOrder) {
		t.Fatalf("titlebar Tab order=%v does not follow visual order %v", order, titlebarOrder)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if hit, ok := focusedControl(t, driver); !ok || hit.ID != order[len(order)-1] {
		t.Fatalf("Shift+Tab focus=%v want %v", hit.ID, order[len(order)-1])
	}
	assertAccessibleControlNames(t, driver, "rich text document")
	clickID(t, driver, readModeID)
}

// TestChromeKeyboardOnlyWalkReachesFindSettingsFileTreeAndConfirmation drives the
// core chrome with the keyboard alone: Ctrl/Cmd+F focuses find and Escape
// restores the editor, the file menu opens settings and Escape returns focus,
// Tab reaches a file tree row and Enter opens it, and the document-close
// confirmation traps focus on its named buttons with Escape returning the draft
// and the focus to the tab.
func TestChromeKeyboardOnlyWalkReachesFindSettingsFileTreeAndConfirmation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keyboard.md")
	if err := os.WriteFile(path, []byte("# Keyboard\n\nBody\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	for range 40 {
		if _, ok := driver.Snapshot().Focused.In(fileRows); ok {
			break
		}
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	row, ok := focusedControl(t, driver)
	if !ok || row.Label != "keyboard.md" {
		t.Fatalf("file tree focus=%+v", row)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Path != filepath.Join(root, "keyboard.md") && state.session.Current().Title != "keyboard.md" {
		t.Fatalf("keyboard tree open path=%q", state.session.Current().Path)
	}

	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierAlt, 'f'); err != nil {
		t.Fatal(err)
	}
	if !state.findOpen || driver.Snapshot().Focused != findInputID {
		t.Fatalf("find shortcut open=%v focus=%v", state.findOpen, driver.Snapshot().Focused)
	}
	if err := driver.Type("Body"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.findOpen || driver.Snapshot().Focused != sourceID {
		t.Fatalf("find Escape open=%v focus=%v", state.findOpen, driver.Snapshot().Focused)
	}

	for range 40 {
		if driver.Snapshot().Focused == fileMenuID {
			break
		}
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !state.fileMenuOpen {
		t.Fatal("keyboard Enter did not open the file menu")
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != recentMenuID {
		t.Fatalf("menu End focus=%v", driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyUp, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !state.settingsOpen || driver.Snapshot().Focused != settingsCloseID {
		t.Fatalf("keyboard settings open=%v focus=%v", state.settingsOpen, driver.Snapshot().Focused)
	}
	var labels []string
	for range 12 {
		hit, ok := focusedControl(t, driver)
		if !ok {
			break
		}
		labels = append(labels, hit.Label)
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		if driver.Snapshot().Focused == settingsCloseID {
			break
		}
	}
	wantSettings := []string{"使用浅色外观", "使用深色外观", "减小源码字号", "增大源码字号", "减小阅读字号", "增大阅读字号", "标准阅读主题", "柔和阅读主题", "自动保存"}
	next := 0
	for _, label := range labels {
		if next < len(wantSettings) && label == wantSettings[next] {
			next++
		}
	}
	if next != len(wantSettings) {
		t.Fatalf("settings Tab order=%v does not reach %v", labels, wantSettings)
	}
	assertAccessibleControlNames(t, driver, "settings")
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.settingsOpen || driver.Snapshot().Focused != fileMenuID {
		t.Fatalf("settings Escape open=%v focus=%v", state.settingsOpen, driver.Snapshot().Focused)
	}

	// Focus the editor, make the document dirty, then close it from the keyboard.
	for range 40 {
		if driver.Snapshot().Focused == sourceID {
			break
		}
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if !state.session.Current().Dirty() {
		t.Fatal("keyboard input did not dirty the document")
	}
	draft := state.session.Current().Text()
	closeTab := tabClose.At(state.session.Active)
	tabIndex := state.session.Active
	for range 40 {
		if driver.Snapshot().Focused == closeTab {
			break
		}
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	if driver.Snapshot().Focused != closeTab {
		t.Fatalf("never reached tab close control")
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Pending() == nil {
		t.Fatal("keyboard tab close did not open the confirmation")
	}
	if driver.Snapshot().Focused != cancelConfirmID {
		t.Fatalf("confirmation initial focus=%v want cancel", driver.Snapshot().Focused)
	}
	assertAccessibleControlNames(t, driver, "confirmation")
	for index, want := range []gfx.ID{discardConfirmID, saveConfirmID, cancelConfirmID} {
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		if got := driver.Snapshot().Focused; got != want {
			t.Fatalf("confirmation Tab %d focus=%v want %v", index, got, want)
		}
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Pending() != nil || state.session.Current().Text() != draft || !state.session.Current().Dirty() {
		t.Fatalf("confirmation Escape changed the draft or kept it pending")
	}
	if driver.Snapshot().Focused != tabClose.At(tabIndex) {
		t.Fatalf("confirmation Escape focus=%v want tab close", driver.Snapshot().Focused)
	}
}
