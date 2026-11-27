package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRichHTMLIMEUndoRestoresSelectionAfterChangingLeaf(test *testing.T) {
	for _, scenario := range []struct {
		name                    string
		backward, replacedOwner bool
	}{
		{name: "forward"},
		{name: "backward", backward: true},
		{name: "same-bytes-replaced-source-editor", replacedOwner: true},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			filename := filepath.Join(context.TempDir(), "choice.mh")
			const source = "<p data-keep='yes'>abc</p>\r\n<p>Second</p>\r\n<!-- preserve -->\r\n"
			const committed = "<p data-keep='yes'>a\u4f60c</p>\r\n<p>Second</p>\r\n<!-- preserve -->\r\n"
			const bom = "\xef\xbb\xbf"
			if err := os.WriteFile(filename, []byte(bom+source), 0640); err != nil {
				context.Fatal(err)
			}
			state, driver := testDriver(context)
			if !state.openFile(filename) {
				context.Fatal(state.message)
			}
			if err := driver.Resize(1200, 780); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, htmlLeafRows.At(0))
			initialRange := text.Range{Start: 1, End: 2}
			if scenario.backward {
				initialRange.Start, initialRange.End = initialRange.End, initialRange.Start
			}
			state.html.input.SetSelection(initialRange)
			if _, err := state.onHTMLTextEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "\u4f60", MarkedSelection: app.TextRange{Start: 1, End: 1}}); err != nil {
				context.Fatal(err)
			}
			if _, err := state.onHTMLTextEvent(app.TextEvent{Kind: app.TextUnmark}); err != nil {
				context.Fatal(err)
			}
			current := state.session.Current()
			if current.Text() != committed {
				context.Fatalf("same-text commit lost source: %q", current.Text())
			}
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, htmlLeafRows.At(1))
			if !state.html.active || state.html.input.String() != "Second" {
				context.Fatal("could not switch to the second paragraph")
			}
			if scenario.replacedOwner {
				current.Editor = text.NewEditor(source)
				current.Replace(committed)
				if _, err := state.onHTMLTextEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo}); err != nil {
					context.Fatal(err)
				}
			} else if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			if current.Text() != source {
				context.Fatalf("undo changed incorrect source bytes: %q", current.Text())
			}
			if scenario.replacedOwner {
				if !state.html.active || state.html.input.String() != "Second" {
					context.Fatal("stale composition selection was restored for a replaced source editor")
				}
				return
			}
			if !state.html.active || state.html.input.String() != "abc" || state.html.input.Selection() != (text.Range{Start: 1, End: 2}) || state.html.input.Caret() != initialRange.End {
				context.Fatalf("undo did not restore the original leaf and oriented selection: block=%d visible=%q selection=%+v caret=%d", state.html.blockIndex, state.html.input.String(), state.html.input.Selection(), state.html.input.Caret())
			}
			if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
				context.Fatal(err)
			}
			if current.Text() != committed || state.html.input.String() != "a\u4f60c" || state.html.input.Caret() != 2 {
				context.Fatalf("redo did not return to the committed leaf: source=%q visible=%q caret=%d", current.Text(), state.html.input.String(), state.html.input.Caret())
			}
			if bytes, err := os.ReadFile(filename); err != nil || string(bytes) != bom+source {
				context.Fatalf("history unexpectedly wrote disk: %q, %v", bytes, err)
			}
		})
	}
}
