package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// compositionCancelCase is one native editing surface and the text-event
// handler that owns its input.
type compositionCancelCase struct {
	name         string
	source       string
	commitText   string
	commitSource string
	ext          string
	fixture      string
	sourceMode   bool
	activate     func(t *testing.T, state *model, driver *app.Driver)
	handler      func(state *model, event app.TextEvent) (app.TextState, error)
	editor       func(state *model) *text.Editor
}

func (c compositionCancelCase) open(t *testing.T) (*model, *app.Driver, string, []byte) {
	t.Helper()
	var data []byte
	var err error
	if c.fixture != "" {
		data, err = os.ReadFile(filepath.Join("..", "..", "internal", "richedit", "testdata", c.fixture))
	} else {
		data = []byte(c.source)
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "composition-cancel"+c.ext)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	state.outline = true
	state.mode = renderedMode
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	if c.sourceMode {
		clickID(t, driver, editModeID)
		if state.mode != editMode {
			t.Fatalf("source mode did not open: %v", state.mode)
		}
	}
	c.activate(t, state, driver)
	return state, driver, path, data
}

// cancelCompositionCases covers every native surface that takes marked text:
// the rendered paragraph projection, a fenced code block, a table cell, a
// .mh rich leaf and the Markdown source editor.
var cancelCompositionCases = []compositionCancelCase{
	{
		name:         "rendered paragraph",
		source:       "# First\n\nParagraph\n\nSecond\n",
		commitText:   "Xaragraph",
		commitSource: "# First\n\nXaragraph\n\nSecond\n",
		ext:          ".md",
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, renderedRows.At(1))
			if !state.rich.active {
				t.Fatal("rendered paragraph did not enter native input")
			}
		},
		handler: func(state *model, event app.TextEvent) (app.TextState, error) {
			return state.onRenderedTextEvent(event)
		},
		editor: func(state *model) *text.Editor { return state.rich.input },
	},
	{
		name:         "fenced code block",
		source:       "# First\n\n```go\ncode\n```\n\nSecond\n",
		commitText:   "Xode",
		commitSource: "# First\n\n```go\nXode\n```\n\nSecond\n",
		ext:          ".md",
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, renderedRows.At(1))
			if !state.codeBlock.active {
				t.Fatalf("code block did not activate: %+v", state.codeBlock)
			}
		},
		handler: func(state *model, event app.TextEvent) (app.TextState, error) {
			return state.onCodeBlockTextEvent(event)
		},
		editor: func(state *model) *text.Editor { return state.codeBlock.input },
	},
	{
		name:         "table cell",
		source:       "| A | B |\n| --- | --- |\n| 1 | 2 |\n",
		commitText:   "X",
		commitSource: "| A | B |\n| --- | --- |\n| X | 2 |\n",
		ext:          ".md",
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, renderedTableCells.At(2))
			if !state.table.active {
				t.Fatalf("table cell did not activate: %+v", state.table)
			}
		},
		handler: func(state *model, event app.TextEvent) (app.TextState, error) {
			return state.onTableCellTextEvent(event)
		},
		editor: func(state *model) *text.Editor { return state.table.input },
	},
	{
		name:         "rich html leaf",
		fixture:      "Real-MkDown.mh",
		commitText:   "X\u5b9e\u7801\u514b\u6837\u672c",
		commitSource: "<p><strong>X\u5b9e\u7801\u514b\u6837\u672c</strong></p><p>\u6b63\u6587 A & B</p>",
		ext:          ".mh",
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, htmlLeafRows.At(0))
			if !state.html.active {
				t.Fatal("rich leaf did not enter native input")
			}
		},
		handler: func(state *model, event app.TextEvent) (app.TextState, error) {
			return state.onHTMLTextEvent(event)
		},
		editor: func(state *model) *text.Editor { return state.html.input },
	},
	{
		name:         "markdown source",
		source:       "# First\n\nParagraph\n",
		commitText:   "X First\n\nParagraph\n",
		commitSource: "X First\n\nParagraph\n",
		ext:          ".md",
		sourceMode:   true,
		activate: func(t *testing.T, state *model, driver *app.Driver) {
			clickID(t, driver, sourceID)
			if driver.Snapshot().Focused != sourceID {
				t.Fatalf("source editor did not take focus: %v", driver.Snapshot().Focused)
			}
		},
		handler: func(state *model, event app.TextEvent) (app.TextState, error) {
			return state.onMarkdownSourceTextEvent(event)
		},
		editor: func(state *model) *text.Editor { return state.session.Current().Editor },
	},
}

// TestCompositionCancellationRestoresEveryNativeEditor pins the application
// half of the cancellation contract: a marked string stays a composition even
// when the input method sends an empty one, and only the explicit cancel event
// ends it, restores the editor and allows saving again.
func TestCompositionCancellationRestoresEveryNativeEditor(t *testing.T) {
	for _, test := range cancelCompositionCases {
		t.Run(test.name, func(t *testing.T) {
			state, driver, path, original := test.open(t)
			document := state.session.Current()
			sourceBefore := document.Text()
			editor := test.editor(state)
			if editor == nil {
				t.Fatal("the editor under test is not active")
			}
			textBefore, selectionBefore := editor.String(), editor.Selection()
			stateBefore := app.TextStateOf(editor)
			if err := driver.Compose("ni"); err != nil {
				t.Fatal(err)
			}
			if state.pendingCompositionFocus() == (gfx.ID{}) {
				t.Fatal("marked text did not register as a pending composition")
			}
			if !test.sourceMode {
				if got := document.Text(); got != sourceBefore {
					t.Fatalf("preedit reached the source: %q", got)
				}
			}

			// An ordinary empty marked string is still a composition; only the
			// explicit cancel may end it.
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextSetMarked, Text: ""}); err != nil {
				t.Fatal(err)
			}
			if state.pendingCompositionFocus() == (gfx.ID{}) {
				t.Fatal("an ordinary empty mark was treated as a cancellation")
			}
			if !state.documentComposing(document) {
				t.Fatal("an ordinary empty mark left no pending composition")
			}

			if _, err := test.handler(state, app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
				t.Fatal(err)
			}
			if state.pendingCompositionFocus() != (gfx.ID{}) {
				t.Fatal("a composition is still pending after an explicit cancel")
			}
			if state.documentComposing(document) {
				t.Fatal("the document still reports a composition after an explicit cancel")
			}
			if _, composing := editor.Composition(); composing {
				t.Fatal("the editor still reports a composition after an explicit cancel")
			}
			if got := editor.String(); got != textBefore {
				t.Fatalf("cancel left editor text %q, want %q", got, textBefore)
			}
			if got := editor.Selection(); got != selectionBefore {
				t.Fatalf("cancel left selection %+v, want %+v", got, selectionBefore)
			}
			stateAfter := app.TextStateOf(editor)
			if stateAfter.Value != stateBefore.Value || stateAfter.Selection != stateBefore.Selection || stateAfter.SelectionReversed != stateBefore.SelectionReversed {
				t.Fatalf("cancel left state %+v, want %+v", stateAfter, stateBefore)
			}
			if got := document.Text(); got != sourceBefore {
				t.Fatalf("cancel changed the source: %q", got)
			}
			if document.Dirty() {
				t.Fatal("cancel dirtied the document")
			}
			if err := state.session.Save(state.session.Active, ""); err != nil {
				t.Fatalf("save after cancel: %v", err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(saved, original) {
				t.Fatalf("cancel plus save rewrote the file: %q", saved)
			}
		})
	}
}

// TestCancelledCompositionRestoresTheReplacedSelection starts a composition
// over a selected rune the way an input method reports its replacement and
// checks that cancelling brings the same selection back, including its
// direction, instead of collapsing or flipping it.
func TestCancelledCompositionRestoresTheReplacedSelection(t *testing.T) {
	for _, test := range []struct {
		name      string
		selection text.Range
		caret     int
		reversed  bool
	}{
		{name: "forward", selection: text.Range{Start: 1, End: 2}, caret: 2},
		{name: "backwards", selection: text.Range{Start: 2, End: 1}, caret: 1, reversed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver := compositionDriver(t, "abc\n")
			clickID(t, driver, renderedRows.At(0))
			input := state.rich.input
			input.SetSelection(test.selection)
			if _, err := state.onRenderedTextEvent(app.TextEvent{
				Kind:           app.TextSetMarked,
				Text:           "ni",
				HasReplacement: true,
				Replacement:    app.TextRange{Start: 1, End: 2},
			}); err != nil {
				t.Fatal(err)
			}
			if input.String() != "anic" {
				t.Fatalf("marked text replaced the selected rune wrongly: %q", input.String())
			}
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
				t.Fatal(err)
			}
			if got := input.String(); got != "abc" {
				t.Fatalf("cancel left %q, want abc", got)
			}
			if got := input.Selection(); got != (text.Range{Start: 1, End: 2}) {
				t.Fatalf("cancel left selection %+v, want the replaced rune", got)
			}
			got := app.TextStateOf(input)
			caret := got.Selection.End
			if got.SelectionReversed {
				caret = got.Selection.Start
			}
			if got.SelectionReversed != test.reversed || caret != test.caret {
				t.Fatalf("cancel left direction reversed=%v caret=%d, want reversed=%v caret=%d", got.SelectionReversed, caret, test.reversed, test.caret)
			}
			if state.session.Current().Text() != "abc\n" {
				t.Fatalf("cancel changed the source: %q", state.session.Current().Text())
			}
		})
	}
}

// TestCancelledCompositionAfterExplicitMarkedReplacementRestoresOriginal pins
// the reported defect: a marked update that names the active composition's range
// must continue the composition, not end it, so cancel restores the text the
// first marked update replaced and nothing reaches the disk.
func TestCancelledCompositionAfterExplicitMarkedReplacementRestoresOriginal(t *testing.T) {
	for _, test := range cancelCompositionCases {
		t.Run(test.name, func(t *testing.T) {
			state, _, path, original := test.open(t)
			document := state.session.Current()
			editor := test.editor(state)
			if editor == nil {
				t.Fatal("the editor under test is not active")
			}
			before := editor.String()
			editor.SetSelection(text.Range{Start: 0, End: 1})
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}}); err != nil {
				t.Fatal(err)
			}
			marked, composing := editor.Composition()
			if !composing {
				t.Fatal("the first marked update did not start a composition")
			}
			if _, err := test.handler(state, app.TextEvent{
				Kind:            app.TextSetMarked,
				Text:            "hao",
				MarkedSelection: app.TextRange{Start: 3, End: 3},
				HasReplacement:  true,
				Replacement:     app.TextRange{Start: marked.Start, End: marked.End},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
				t.Fatal(err)
			}
			if got := editor.String(); got != before {
				t.Fatalf("cancel after a continued composition left editor %q, want %q (document %q, dirty=%v)", got, before, document.Text(), document.Dirty())
			}
			if state.pendingCompositionFocus() != (gfx.ID{}) {
				t.Fatal("a composition is still pending after an explicit cancel")
			}
			if document.Dirty() {
				t.Fatal("cancel dirtied the document")
			}
			if err := state.session.Save(state.session.Active, ""); err != nil {
				t.Fatalf("save after cancel: %v", err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(saved, original) {
				t.Fatalf("cancel plus save changed disk bytes: got=%q want=%q", saved, original)
			}
		})
	}
}

// TestExplicitMarkedCommitUndoRedoSaveRestoresOriginal pins the commit half of
// the same contract: an input method that commits with an explicit replacement
// range stays in the composition's undo unit, so one Undo restores the text the
// first marked update replaced, Redo re-applies the commit, and a save only ever
// sees the original or committed text — never the preedit.
func TestExplicitMarkedCommitUndoRedoSaveRestoresOriginal(t *testing.T) {
	for _, test := range cancelCompositionCases {
		t.Run(test.name, func(t *testing.T) {
			state, driver, path, original := test.open(t)
			editor := test.editor(state)
			if editor == nil {
				t.Fatal("the editor under test is not active")
			}
			before := editor.String()
			assertSavedState := func(stage, wantText, wantSource string) {
				t.Helper()
				if err := driver.Resize(1201, 780); err != nil {
					t.Fatal(err)
				}
				input := test.editor(state)
				if input == nil {
					t.Fatalf("%s lost its editor", stage)
				}
				if got := input.String(); got != wantText {
					t.Fatalf("%s text = %q, want %q", stage, got, wantText)
				}
				if _, composing := input.Composition(); composing {
					t.Fatalf("%s left an active composition", stage)
				}
				document := state.session.Current()
				if got := document.Text(); got != wantSource {
					t.Fatalf("%s source = %q, want %q", stage, got, wantSource)
				}
				if err := state.session.Save(state.session.Active, ""); err != nil {
					t.Fatalf("%s save: %v", stage, err)
				}
				saved, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(saved, []byte(wantSource)) {
					t.Fatalf("%s saved bytes = %q, want %q", stage, saved, wantSource)
				}
				if document.Dirty() {
					t.Fatalf("%s save left the document dirty", stage)
				}
			}
			editor.SetSelection(text.Range{Start: 0, End: 1})
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}}); err != nil {
				t.Fatal(err)
			}
			marked, composing := editor.Composition()
			if !composing {
				t.Fatal("the marked update did not start a composition")
			}
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextInsert, Text: "X", HasReplacement: true, Replacement: app.TextRange{Start: marked.Start, End: marked.End}}); err != nil {
				t.Fatal(err)
			}
			assertSavedState("commit", test.commitText, test.commitSource)
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo}); err != nil {
				t.Fatal(err)
			}
			assertSavedState("undo", before, string(original))
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextRedo}); err != nil {
				t.Fatal(err)
			}
			assertSavedState("redo", test.commitText, test.commitSource)
			if _, err := test.handler(state, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo}); err != nil {
				t.Fatal(err)
			}
			assertSavedState("second undo", before, string(original))
		})
	}
}
