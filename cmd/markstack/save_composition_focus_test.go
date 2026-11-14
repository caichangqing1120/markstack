package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

// compositionFocusByCase names the native editor each existing composition
// fixture activates. The save-menu regressions compare the driver's real focus
// owner against these, not just against whatever the application reports.
var compositionFocusByCase = map[string]gfx.ID{
	"rendered paragraph": richInputID,
	"fenced code block":  codeInputID,
	"table cell":         tableInputID,
	"rich html leaf":     htmlInputID,
	"markdown source":    sourceID,
	"code source":        sourceID,
	"plain source":       sourceID,
}

// sourceKindCompositionCases extend the shared composition fixtures to the
// source editor used by code and plain documents when no projection is active.
var sourceKindCompositionCases = []compositionCancelCase{
	{
		name:       "code source",
		source:     "package main\n\nfunc main() {}\n",
		ext:        ".go",
		sourceMode: true,
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, sourceID)
			if driver.Snapshot().Focused != sourceID {
				t.Fatalf("code source did not take focus: %v", driver.Snapshot().Focused)
			}
		},
		editor: func(state *model) *text.Editor { return state.session.Current().Editor },
	},
	{
		name:       "plain source",
		source:     "abc **literal** # not a heading\n\nuntouched tail\n",
		ext:        ".txt",
		sourceMode: true,
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, sourceID)
			if driver.Snapshot().Focused != sourceID {
				t.Fatalf("plain source did not take focus: %v", driver.Snapshot().Focused)
			}
		},
		editor: func(state *model) *text.Editor { return state.session.Current().Editor },
	},
}

// requireTextState compares the complete application-owned native state of an
// editor so a blocked save cannot quietly clear marked text or its selection.
func requireTextState(t *testing.T, stage string, editor *text.Editor, want app.TextState) {
	t.Helper()
	if got := app.TextStateOf(editor); got != want {
		t.Fatalf("%s text state = %+v, want %+v", stage, got, want)
	}
}

// requireNativeComposition proves the driver still has the same composing
// native text input, not merely a model flag after a later focus request.
func requireNativeComposition(t *testing.T, driver *app.Driver, owner gfx.ID, want app.TextState, stage string) {
	t.Helper()
	snapshot := driver.Snapshot()
	if snapshot.Focused != owner {
		t.Fatalf("%s left focus on %v, want the composition owner %v", stage, snapshot.Focused, owner)
	}
	if snapshot.Editor == nil {
		t.Fatalf("%s left no native text input focused", stage)
	}
	if !snapshot.Editor.Composing {
		t.Fatalf("%s ended the native composition", stage)
	}
	if snapshot.Editor.Value != want.Value ||
		[2]int{snapshot.Editor.Selection[0], snapshot.Editor.Selection[1]} != [2]int{want.Selection.Start, want.Selection.End} ||
		[2]int{snapshot.Editor.Composition[0], snapshot.Editor.Composition[1]} != [2]int{want.Composition.Start, want.Composition.End} {
		t.Fatalf("%s native editor = %+v, want %+v", stage, *snapshot.Editor, want)
	}
}

func compositionParts(state app.TextState) (before, marked, after string) {
	runes := []rune(state.Value)
	start := max(0, min(state.Composition.Start, len(runes)))
	end := max(start, min(state.Composition.End, len(runes)))
	return string(runes[:start]), string(runes[start:end]), string(runes[end:])
}

// requireCompositionExtended keeps the marked spelling and the text around it,
// so continuation cannot silently restart at "hao" instead of "nihao".
func requireCompositionExtended(t *testing.T, stage string, previous, next app.TextState) {
	t.Helper()
	before, marked, after := compositionParts(previous)
	nextBefore, nextMarked, nextAfter := compositionParts(next)
	if nextBefore != before || nextAfter != after {
		t.Fatalf("%s replaced the composition context: %q -> %q", stage, previous.Value, next.Value)
	}
	if !strings.HasPrefix(nextMarked, marked) || nextMarked == marked {
		t.Fatalf("%s marked text = %q, want an extension of %q", stage, nextMarked, marked)
	}
}

func hitBounds(t *testing.T, driver *app.Driver, id gfx.ID) gfx.Rect {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			return hit.Bounds
		}
	}
	t.Fatalf("control %v is not visible", id)
	return gfx.Rect{}
}

func pressID(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	bounds := hitBounds(t, driver, id)
	if err := driver.Press(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
}

func releaseID(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	bounds := hitBounds(t, driver, id)
	if err := driver.Release(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
}

// cancelCompositionFixture restores the model after focus and continuation checks.
// Driver has no explicit native cancel event; real Escape requires native acceptance.
func cancelCompositionFixture(t *testing.T, state *model, test compositionCancelCase) {
	t.Helper()
	if test.handler != nil {
		if _, err := test.handler(state, app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
			t.Fatal(err)
		}
		return
	}
	editor := test.editor(state)
	if editor == nil {
		t.Fatal("the restored composition owner has no editor")
	}
	editor.CancelComposition()
}

// pumpQueuedFrame consumes the repaint a blocked menu save must request. The
// action runs after the paint pass has already handled focus, so without this
// follow-up frame the queued request would never reach the native text input.
func pumpQueuedFrame(t *testing.T, driver *app.Driver, stage string) {
	t.Helper()
	select {
	case <-driver.Wakes():
	default:
		t.Fatalf("%s did not request the follow-up frame that restores input focus", stage)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
}

// TestBlockedFileMenuSaveKeepsCompositionAndFocus pins the reported native
// defect: choosing Save or Save As from the File menu while marked text is
// pending must leave the file and source untouched and must keep (or give back)
// the native text input that owns the composition. Without that focus the next
// Escape cannot cancel the stalled preedit.
func TestBlockedFileMenuSaveKeepsCompositionAndFocus(t *testing.T) {
	cases := append([]compositionCancelCase{}, cancelCompositionCases...)
	cases = append(cases, sourceKindCompositionCases...)
	for _, test := range cases {
		for _, action := range []struct {
			name string
			id   gfx.ID
		}{
			{name: "save", id: saveID},
			{name: "save as", id: saveAsID},
		} {
			t.Run(test.name+"/"+action.name, func(t *testing.T) {
				state, driver, path, original := test.open(t)
				document := state.session.Current()
				editor := test.editor(state)
				if editor == nil {
					t.Fatal("the editor under test is not active")
				}
				owner, ok := compositionFocusByCase[test.name]
				if !ok {
					t.Fatalf("no expected focus owner for %q", test.name)
				}
				sourceBefore := document.Editor.CommittedString()
				dirtyBefore := document.Dirty()
				editorBefore := app.TextStateOf(editor)

				if err := driver.Compose("ni"); err != nil {
					t.Fatal(err)
				}
				initial := app.TextStateOf(editor)
				if !initial.Composing {
					t.Fatal("the fixture did not start a native composition")
				}
				if got := state.pendingCompositionFocus(); got != owner {
					t.Fatalf("composition owner = %v, want %v", got, owner)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("preedit reached the source: %q", got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("preedit changed dirty state: %v -> %v", dirtyBefore, got)
				}

				pressID(t, driver, fileMenuID)
				if state.fileMenuOpen {
					t.Fatal("the File menu opened on press instead of release")
				}
				requireNativeComposition(t, driver, owner, initial, action.name+" File trigger press")
				releaseID(t, driver, fileMenuID)
				if !state.fileMenuOpen {
					t.Fatal("the File menu did not open")
				}
				if _, requested := driver.LastSaveFileRequest(); requested {
					t.Fatal("opening the File menu asked for a save path")
				}
				requireNativeComposition(t, driver, owner, initial, action.name+" File menu open")
				if err := driver.Compose("nihao"); err != nil {
					t.Fatalf("%s could not continue through the open popup: %v", action.name, err)
				}
				continued := app.TextStateOf(editor)
				if continued.Value == initial.Value {
					t.Fatalf("%s did not extend the composition through the open popup", action.name)
				}
				requireCompositionExtended(t, action.name+" open popup", initial, continued)
				if !state.fileMenuOpen {
					t.Fatalf("%s closed the File menu during composition", action.name)
				}
				requireNativeComposition(t, driver, owner, continued, action.name+" composition while menu open")
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s committed popup composition to the source: %q", action.name, got)
				}

				pressID(t, driver, action.id)
				if !state.fileMenuOpen {
					t.Fatalf("%s closed the File menu on press", action.name)
				}
				requireNativeComposition(t, driver, owner, continued, action.name+" menu item press")
				releaseID(t, driver, action.id)
				if state.fileMenuOpen {
					t.Fatalf("%s left the File menu open", action.name)
				}
				if state.message != session.ErrCompositionPending.Error() {
					t.Fatalf("%s message = %q, want %q", action.name, state.message, session.ErrCompositionPending.Error())
				}
				if _, requested := driver.LastSaveFileRequest(); requested {
					t.Fatalf("%s opened a save path dialog while composing", action.name)
				}
				if got := state.pendingCompositionFocus(); got != owner {
					t.Fatalf("%s composition owner = %v, want %v", action.name, got, owner)
				}
				if !state.documentComposing(document) {
					t.Fatalf("%s cleared the pending composition", action.name)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s committed preedit to the source: %q", action.name, got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("%s changed dirty state: %v -> %v", action.name, dirtyBefore, got)
				}
				requireTextState(t, action.name, editor, continued)
				if saved, err := os.ReadFile(path); err != nil {
					t.Fatal(err)
				} else if !bytes.Equal(saved, original) {
					t.Fatalf("%s changed the file while composing: %q", action.name, saved)
				}

				pumpQueuedFrame(t, driver, action.name)
				requireNativeComposition(t, driver, owner, continued, action.name)

				if err := driver.Compose("nihaoma"); err != nil {
					t.Fatalf("%s could not continue through the restored input: %v", action.name, err)
				}
				restored := app.TextStateOf(editor)
				if restored.Value == continued.Value {
					t.Fatalf("%s did not extend the composition after the rejected save", action.name)
				}
				requireCompositionExtended(t, action.name+" rejected save", continued, restored)
				requireNativeComposition(t, driver, owner, restored, action.name+" after rejected save")
				if got := state.pendingCompositionFocus(); got != owner {
					t.Fatalf("%s lost the composition after continuing: %v", action.name, got)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s committed continued preedit to the source: %q", action.name, got)
				}
				if driver.Snapshot().Focused != owner {
					t.Fatalf("%s focus left the composition owner before cancellation", action.name)
				}
				cancelCompositionFixture(t, state, test)
				if got := state.pendingCompositionFocus(); got != (gfx.ID{}) {
					t.Fatalf("%s still has a pending composition: %v", action.name, got)
				}
				if state.documentComposing(document) {
					t.Fatalf("%s still reports a document composition", action.name)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s cancellation changed the source: %q", action.name, got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("%s cancellation changed dirty state: %v -> %v", action.name, dirtyBefore, got)
				}
				requireTextState(t, action.name+" cancel", editor, editorBefore)

				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					t.Fatal(err)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s undo after cancellation changed the source: %q", action.name, got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("%s undo after cancellation changed dirty state: %v -> %v", action.name, dirtyBefore, got)
				}
				requireTextState(t, action.name+" undo after cancel", editor, editorBefore)
				if saved, err := os.ReadFile(path); err != nil {
					t.Fatal(err)
				} else if !bytes.Equal(saved, original) {
					t.Fatalf("%s cancellation changed the file: %q", action.name, saved)
				}
			})
		}
	}
}

// focusMenuActionByTab moves focus off the composing input and onto a menu row.
// Driver has no accessibility-action entry point, so this public traversal +
// Enter path is the closest available model of AX focus followed by activation:
// the action is delivered without Config.OnPointer.
func focusMenuActionByTab(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	for index := 0; index < 80; index++ {
		if driver.Snapshot().Focused == id {
			return
		}
		if err := driver.Key(gfx.KeyTab, gfx.ModifierControl); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("focus never reached %v", id)
}

// TestBlockedFileMenuSaveRestoresFocusAfterDirectActivation covers the AX
// activation order: focus a menu row while the composition owns the editor,
// then activate it without a pointer event. Driver exposes no accessibility
// action, so focus traversal + Enter is a public-path approximation, not a
// native AX proof.
func TestBlockedFileMenuSaveRestoresFocusAfterDirectActivation(t *testing.T) {
	cases := append([]compositionCancelCase{}, cancelCompositionCases...)
	cases = append(cases, sourceKindCompositionCases...)
	for _, test := range cases {
		for _, action := range []struct {
			name string
			id   gfx.ID
		}{
			{name: "save", id: saveID},
			{name: "save as", id: saveAsID},
		} {
			t.Run(test.name+"/"+action.name, func(t *testing.T) {
				state, driver, path, original := test.open(t)
				document := state.session.Current()
				editor := test.editor(state)
				if editor == nil {
					t.Fatal("the editor under test is not active")
				}
				owner, ok := compositionFocusByCase[test.name]
				if !ok {
					t.Fatalf("no expected focus owner for %q", test.name)
				}
				sourceBefore := document.Editor.CommittedString()
				dirtyBefore := document.Dirty()
				editorBefore := app.TextStateOf(editor)
				if err := driver.Compose("ni"); err != nil {
					t.Fatal(err)
				}
				composed := app.TextStateOf(editor)
				if !composed.Composing {
					t.Fatal("the fixture did not start a native composition")
				}

				clickID(t, driver, fileMenuID)
				if !state.fileMenuOpen {
					t.Fatal("the File menu did not open")
				}
				focusMenuActionByTab(t, driver, action.id)
				if got := driver.Snapshot().Focused; got != action.id {
					t.Fatalf("focused %v, want menu action %v", got, action.id)
				}
				if driver.Snapshot().Editor != nil {
					t.Fatal("focusing the menu action did not leave the editor")
				}

				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					t.Fatal(err)
				}
				if state.fileMenuOpen {
					t.Fatalf("%s left the File menu open", action.name)
				}
				if state.message != session.ErrCompositionPending.Error() {
					t.Fatalf("%s message = %q, want %q", action.name, state.message, session.ErrCompositionPending.Error())
				}
				if _, requested := driver.LastSaveFileRequest(); requested {
					t.Fatalf("%s opened a save path dialog while composing", action.name)
				}
				requireNativeComposition(t, driver, owner, composed, action.name+" direct activation")
				if got := state.pendingCompositionFocus(); got != owner {
					t.Fatalf("%s composition owner = %v, want %v", action.name, got, owner)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s committed preedit to the source: %q", action.name, got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("%s changed dirty state: %v -> %v", action.name, dirtyBefore, got)
				}
				requireTextState(t, action.name, editor, composed)
				if saved, err := os.ReadFile(path); err != nil {
					t.Fatal(err)
				} else if !bytes.Equal(saved, original) {
					t.Fatalf("%s changed the file while composing: %q", action.name, saved)
				}

				if err := driver.Compose("nihao"); err != nil {
					t.Fatalf("%s could not continue after direct activation: %v", action.name, err)
				}
				continued := app.TextStateOf(editor)
				requireCompositionExtended(t, action.name+" direct activation continuation", composed, continued)
				requireNativeComposition(t, driver, owner, continued, action.name+" direct activation continuation")
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s committed continued preedit to the source: %q", action.name, got)
				}

				cancelCompositionFixture(t, state, test)
				if got := state.pendingCompositionFocus(); got != (gfx.ID{}) {
					t.Fatalf("%s still has a pending composition: %v", action.name, got)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s cancellation changed the source: %q", action.name, got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("%s cancellation changed dirty state: %v -> %v", action.name, dirtyBefore, got)
				}
				requireTextState(t, action.name+" cancel", editor, editorBefore)

				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					t.Fatal(err)
				}
				if got := document.Editor.CommittedString(); got != sourceBefore {
					t.Fatalf("%s undo after cancellation changed the source: %q", action.name, got)
				}
				if got := document.Dirty(); got != dirtyBefore {
					t.Fatalf("%s undo after cancellation changed dirty state: %v -> %v", action.name, dirtyBefore, got)
				}
				requireTextState(t, action.name+" undo after cancel", editor, editorBefore)
				if saved, err := os.ReadFile(path); err != nil {
					t.Fatal(err)
				} else if !bytes.Equal(saved, original) {
					t.Fatalf("%s cancellation changed the file: %q", action.name, saved)
				}
			})
		}
	}
}
