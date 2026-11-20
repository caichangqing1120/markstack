package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
)

func TestConfirmationBlocksApplicationShortcuts(t *testing.T) {
	for _, chord := range []struct {
		name  string
		key   rune
		shift bool
	}{{"new", 'n', false}, {"open", 'o', false}, {"save", 's', false}, {"save-as", 's', true}, {"split", '2', false}, {"find", 'f', false}} {
		t.Run(chord.name, func(t *testing.T) {
			state, driver := testDriver(t)
			document := state.session.Current()
			document.Replace("keep this draft")
			clickID(t, driver, tabClose.At(0))
			modifiers := primaryModifier()
			if chord.shift {
				modifiers |= gfx.ModifierShift
			}
			if _, err := driver.ShortcutChord(modifiers, chord.key); err != nil {
				t.Fatal(err)
			}
			if len(state.session.Tabs) != 1 || state.session.Current() != document || state.mode != editMode || state.message != "" {
				t.Fatalf("shortcut escaped confirmation: tabs=%d mode=%v message=%q", len(state.session.Tabs), state.mode, state.message)
			}
			if state.session.Pending() == nil || document.Text() != "keep this draft" {
				t.Fatal("shortcut changed the pending draft")
			}
		})
	}
}

func TestFindShortcutRestoresHiddenSidebar(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sidebarToggleID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'f'); err != nil {
		t.Fatal(err)
	}
	if !state.sidebar || driver.Snapshot().Focused != searchID {
		t.Fatalf("find did not expose and focus search: sidebar=%v focus=%v", state.sidebar, driver.Snapshot().Focused)
	}
}

func TestDocumentNavigationClearsStaleSaveStatus(t *testing.T) {
	state, driver := testDriver(t)
	state.message = "已保存 old-document.md"
	clickID(t, driver, newFileID)
	if state.message != "" {
		t.Fatal("new document retained an unrelated save status")
	}
	path := filepath.Join(t.TempDir(), "opened.md")
	if err := os.WriteFile(path, []byte("# Opened"), 0644); err != nil {
		t.Fatal(err)
	}
	state.message = "已保存 old-document.md"
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if state.message != "" {
		t.Fatal("opened document retained an unrelated save status")
	}
}

func TestDirtyDocumentDoesNotDisplaySavedStatus(t *testing.T) {
	state, driver := testDriver(t)
	state.message = "已保存 edited.md"
	state.session.Current().Replace("new unsaved content")
	if err := driver.Resize(1100, 740); err != nil {
		t.Fatal(err)
	}
	if hasText(driver, "已保存 edited.md") {
		t.Fatal("dirty document still claims its latest content was saved")
	}
}

func TestNewDocumentStartsReadyForTyping(t *testing.T) {
	for _, method := range []string{"initial", "button", "shortcut"} {
		t.Run(method, func(t *testing.T) {
			state, driver := testDriver(t)
			switch method {
			case "button":
				clickID(t, driver, newFileID)
			case "shortcut":
				clickID(t, driver, readModeID)
				if _, err := driver.ShortcutChord(primaryModifier(), 'n'); err != nil {
					t.Fatal(err)
				}
			}
			if driver.Snapshot().Focused != sourceID {
				t.Fatalf("new document focus = %v", driver.Snapshot().Focused)
			}
			if err := driver.Type("Ready"); err != nil || state.session.Current().Text() != "Ready" {
				t.Fatalf("typing after new = %q, %v", state.session.Current().Text(), err)
			}
		})
	}
}

func TestTemplateMenuCreatesFocusedUnsavedDraftsAtNarrowWidth(t *testing.T) {
	state, driver := testDriver(t)
	first := state.session.Current()
	first.Replace("existing unsaved")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	for _, id := range []gfx.ID{openFolderID, openFileID, saveID, saveAsID, recentMenuID, noteTemplateID, proposalTemplateID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID != id {
				continue
			}
			found = true
			if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 740 || hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > 520 {
				t.Fatalf("menu control %v out of narrow window: %+v", id, hit.Bounds)
			}
		}
		if !found {
			t.Fatalf("menu control %v missing", id)
		}
	}
	clickID(t, driver, noteTemplateID)
	note := state.session.Current()
	if note.Path != "" || note.Title != "笔记-2.md" || !note.Dirty() || state.mode != editMode || driver.Snapshot().Focused != sourceID {
		t.Fatalf("note template not ready: %+v focus=%v", note, driver.Snapshot().Focused)
	}
	if err := driver.Type("项目"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(note.Text(), "# 项目\n") || first.Text() != "existing unsaved" {
		t.Fatalf("template typing changed drafts: note=%q first=%q", note.Text(), first.Text())
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, proposalTemplateID)
	proposal := state.session.Current()
	if proposal.Title != "方案-3.md" || !proposal.Dirty() || !strings.HasPrefix(proposal.Text(), "# 方案\n") || note.Text() == proposal.Text() {
		t.Fatalf("proposal template=%+v previous=%q", proposal, note.Text())
	}
	clickID(t, driver, tabClose.At(state.session.Active))
	if state.session.Pending() == nil {
		t.Fatal("template close bypassed confirmation")
	}
	clickID(t, driver, cancelConfirmID)
	if state.session.Current() != proposal || !proposal.Dirty() || first.Text() != "existing unsaved" {
		t.Fatal("cancel lost template or original draft")
	}
}

func TestAccessibilityActivationNavigatesAndCreatesDocuments(t *testing.T) {
	state := newModel()
	state.session.Current().Replace("# First\n\n## Second")
	state.outline = true
	var canvas gfx.Canvas
	size := ui.Size{Width: 1200, Height: 780}
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	canvas.BeginEventPass(outlineRows.At(1), gfx.ClickInfo{})
	state.paint(&canvas, size)
	canvas.EndEventPass()
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if state.session.Current().Editor.Caret() != 9 {
		t.Fatalf("accessibility outline activation left caret at %d", state.session.Current().Editor.Caret())
	}
	if driver.Snapshot().Focused != sourceID {
		t.Fatal("accessibility outline activation did not request editing focus")
	}
	canvas.BeginEventPass(newFileID, gfx.ClickInfo{})
	state.paint(&canvas, size)
	canvas.EndEventPass()
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if len(state.session.Tabs) != 2 || state.session.Current().Text() != "" {
		t.Fatal("accessibility new-document activation did nothing")
	}
	if err := driver.Type("Exactly one\nnew document"); err != nil {
		t.Fatal(err)
	}
	if len(state.session.Tabs) != 2 || state.session.Current().Text() != "Exactly one\nnew document" {
		t.Fatal("typing after accessibility activation did not reach the single new document")
	}
}

func TestManyTabsKeepActiveDocumentReachable(t *testing.T) {
	state, driver := testDriver(t)
	for range 18 {
		state.session.NewDocument()
	}
	if err := driver.Resize(1000, 700); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, tabRows.At(state.session.Active))
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == tabRows.At(state.session.Active) && hit.Bounds.X+hit.Bounds.Width > 1000 {
			t.Fatalf("active tab is outside the window: %+v", hit.Bounds)
		}
	}
	for state.session.Active > 0 {
		clickID(t, driver, tabPreviousID)
		clickID(t, driver, tabRows.At(state.session.Active))
	}
}

func TestOutlineSkipsFencedHeadingsAndFocusesEditor(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Actual\n\n```md\n## Not a heading\n```\n\n~~~\n# Also code\n~~~\n\n## Second **title**\n"
	state.session.Current().Replace(source)
	clickID(t, driver, outlineToggleID)
	if len(state.outlines) != 2 || state.outlines[1].Title != "Second title" {
		t.Fatalf("outline = %+v", state.outlines)
	}
	clickID(t, driver, outlineRows.At(1))
	if driver.Snapshot().Focused != sourceID {
		t.Fatalf("outline navigation left focus on %v", driver.Snapshot().Focused)
	}
}

func TestReadModeLinksOpenOnlySafeDestinations(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("[Viva](https://vivaui.dev) [Unsafe](file:///etc/passwd)")
	clickID(t, driver, readModeID)
	var links []gfx.Hit
	for _, hit := range driver.Snapshot().Hits {
		if hit.Role == gfx.RoleLink {
			links = append(links, hit)
		}
	}
	if len(links) != 1 {
		t.Fatalf("got %d interactive links, want only the safe web link", len(links))
	}
	clickID(t, driver, links[0].ID)
	if target, ok := driver.LastOpenedURL(); !ok || target != "https://vivaui.dev" {
		t.Fatalf("opened URL = %q, %v", target, ok)
	}
}

func TestSaveFailureAndCanceledPickerKeepDirtyDocument(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("unsaved body")
	if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, saveID)
	if state.session.Current().Path != "" || !state.session.Current().Dirty() {
		t.Fatal("canceled picker changed the draft")
	}
	path := filepath.Join(t.TempDir(), "missing", "draft.md")
	if err := driver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, saveID)
	if !state.session.Current().Dirty() || state.session.Current().Text() != "unsaved body" || state.message == "" {
		t.Fatal("failed save lost content or failed silently")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed save created a document: %v", err)
	}
}

func TestConfirmationSaveFailureKeepsPendingDraft(t *testing.T) {
	state, driver := testDriver(t)
	document := state.session.Current()
	document.Replace("must survive")
	clickID(t, driver, tabClose.At(0))
	path := filepath.Join(t.TempDir(), "missing", "draft.md")
	if err := driver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, saveConfirmID)
	if state.session.Pending() == nil || state.session.Pending().Document != document || !document.Dirty() || document.Text() != "must survive" {
		t.Fatal("failed confirmation save lost its pending draft")
	}
	clickID(t, driver, cancelConfirmID)
	if state.session.Current() != document {
		t.Fatal("cancel after failed save lost draft")
	}
}

func TestWorkspaceSwitchSavesEveryDirtyTab(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("first draft")
	state.session.NewDocument().Replace("second draft")
	root := t.TempDir()
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	saved := t.TempDir()
	for index, body := range []string{"first draft", "second draft"} {
		if state.session.Pending() == nil || state.session.Pending().Document.Text() != body {
			t.Fatalf("pending document %d incorrect", index)
		}
		path := filepath.Join(saved, strconv.Itoa(index)+".md")
		if err := driver.QueueSaveFile(path, nil); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, saveConfirmID)
		data, err := os.ReadFile(path)
		if err != nil || string(data) != body {
			t.Fatalf("saved draft %d = %q, %v", index, data, err)
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.session.Root != canonical || state.session.Pending() != nil || len(state.session.Tabs) != 1 {
		t.Fatal("workspace transition did not finish after saving both drafts")
	}
}

func TestUndoRedoAndModeChangesPreserveDocument(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sourceID)
	if err := driver.Type("你好 Markdown"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "" {
		t.Fatal("undo failed")
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	for _, id := range []gfx.ID{splitModeID, readModeID, editModeID} {
		clickID(t, driver, id)
	}
	if !strings.Contains(state.session.Current().Text(), "你好 Markdown") || !state.session.Current().Dirty() {
		t.Fatal("mode change lost edits or dirty state")
	}
}

func TestQuitConfirmationCanBeCanceledThenSaved(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("quit draft")
	allowed := false
	newConfig(state).OnCloseRequest(func() { allowed = true })
	if err := driver.Resize(1100, 740); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, cancelConfirmID)
	if allowed || state.session.Pending() != nil {
		t.Fatal("cancel allowed a quit")
	}
	newConfig(state).OnCloseRequest(func() { allowed = true })
	if err := driver.Resize(1100, 741); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "quit.md")
	if err := driver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, saveConfirmID)
	if !allowed || state.session.Pending() != nil {
		t.Fatal("saving quit confirmation did not finish")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "quit draft" {
		t.Fatalf("quit save = %q, %v", data, err)
	}
}

type acceptanceHost struct {
	quitRequest func()
	closed      func()
	quits       int
}

func (host *acceptanceHost) OpenWindow(_ app.Config, _ *gfx.Canvas, closed func()) (app.WindowControl, error) {
	host.closed = closed
	return acceptanceWindow{host}, nil
}

func (host *acceptanceHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	return nil, errors.New("status item is not used in this test")
}

func (host *acceptanceHost) Quit()                                { host.quits++ }
func (host *acceptanceHost) SetQuitRequestHandler(handler func()) { host.quitRequest = handler }
func (host *acceptanceHost) SetDockBadge(string)                  {}
func (host *acceptanceHost) SetProgress(float32)                  {}

func (host *acceptanceHost) Post(fn func()) {
	if fn != nil {
		fn()
	}
}

type acceptanceWindow struct{ host *acceptanceHost }

func (window acceptanceWindow) Close()          { window.host.closed() }
func (acceptanceWindow) Hide()                  {}
func (acceptanceWindow) Show()                  {}
func (acceptanceWindow) Frame() (ui.Rect, bool) { return ui.Rect{}, true }

func TestSystemQuitUsesUnsavedConfirmation(t *testing.T) {
	state := newModel()
	state.session.Current().Replace("protect from system quit")
	host := &acceptanceHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error {
		if err := start(host); err != nil {
			return err
		}
		defer host.closed()
		if host.quitRequest == nil {
			t.Fatal("system quit has no confirmation handler")
		}
		host.quitRequest()
		if host.quits != 0 || state.session.Pending() == nil {
			t.Fatal("system quit bypassed unsaved confirmation")
		}
		state.resolve(session.Cancel)
		if host.quits != 0 || state.session.Current().Text() != "protect from system quit" {
			t.Fatal("canceling system quit lost the draft")
		}
		host.quitRequest()
		state.resolve(session.Discard)
		if host.quits != 1 {
			t.Fatal("confirmed system quit did not end the application")
		}
		return nil
	})
	defer app.UseApplicationRunner(nil)
	if err := runApp(state); err != nil {
		t.Fatal(err)
	}
}
