package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

func TestRenderedFencedCodeIndentKeepsDirectedHistorySelection(test *testing.T) {
	for _, backward := range []bool{false, true} {
		test.Run(fmt.Sprintf("backward-%v", backward), func(context *testing.T) {
			const block = "```go\n\u7532\U0001f600b\nxy\u00e9\n```\n"
			state, driver, path, original := renderedSourceCollisionFixture(context, block+"\nBridge.\n\n"+block+"\nTail.\n", false, 1200)
			clickID(context, driver, renderedRows.At(2))
			before := text.Range{Start: 1, End: 7}
			after := text.Range{Start: 2, End: 9}
			beforeCaret, afterCaret := before.End, after.End
			selection := before
			if backward {
				selection.Start, selection.End = selection.End, selection.Start
				beforeCaret, afterCaret = before.Start, after.Start
			}
			state.codeBlock.input.SetSelection(selection)
			if err := driver.Key(gfx.KeyTab, 0); err != nil {
				context.Fatal(err)
			}
			indented := []byte("\xef\xbb\xbf```go\r\n\u7532\U0001f600b\r\nxy\u00e9\r\n```\r\n\r\nBridge.\r\n\r\n```go\r\n\t\u7532\U0001f600b\r\n\txy\u00e9\r\n```\r\n\r\nTail.\r\n")
			checkSelection := func(label string, want text.Range, caret int) {
				context.Helper()
				input := state.codeBlock.input
				if state.mode != renderedMode || !state.codeBlock.active || state.codeBlock.renderedIndex != 2 || input == nil {
					context.Fatal("lost the second fenced editor")
				}
				if input.Selection() != want || input.Caret() != caret {
					context.Errorf("%s selection=%+v caret=%d; want %+v and %d", label, input.Selection(), input.Caret(), want, caret)
				}
			}
			checkSelection("Tab", after, afterCaret)
			expectRenderedCollisionSave(context, state, driver, path, indented)
			steps := []struct {
				key   rune
				want  text.Range
				caret int
				bytes []byte
			}{{'z', before, beforeCaret, original}, {'y', after, afterCaret, indented}}
			for range 2 {
				for _, entry := range steps {
					if _, err := driver.ShortcutChord(primaryModifier(), entry.key); err != nil {
						context.Fatal(err)
					}
					checkSelection(string(entry.key), entry.want, entry.caret)
					expectRenderedCollisionSave(context, state, driver, path, entry.bytes)
				}
			}
			if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
				context.Fatal(err)
			}
			checkSelection("Shift+Tab", before, beforeCaret)
			expectRenderedCollisionSave(context, state, driver, path, original)
			for index, entry := range steps {
				if _, err := driver.ShortcutChord(primaryModifier(), entry.key); err != nil {
					context.Fatal(err)
				}
				want := steps[1-index]
				checkSelection("outdent "+string(entry.key), want.want, want.caret)
				expectRenderedCollisionSave(context, state, driver, path, want.bytes)
			}
		})
	}
}

func TestRenderedFencedCodeOutdentMapsPartialFinalIndent(test *testing.T) {
	for _, backward := range []bool{false, true} {
		test.Run(fmt.Sprintf("backward-%v", backward), func(context *testing.T) {
			const original = "```go\n    a\n    b\n```\n"
			state, driver, path, originalBytes := renderedSourceCollisionFixture(context, original, false, 1200)
			clickID(context, driver, renderedRows.At(0))
			before, after := text.Range{Start: 4, End: 7}, text.Range{Start: 0, End: 2}
			if backward {
				before.Start, before.End = before.End, before.Start
				after.Start, after.End = after.End, after.Start
			}
			state.codeBlock.input.SetSelection(before)
			if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
				context.Fatal(err)
			}
			outdented := []byte("\xef\xbb\xbf```go\r\na\r\nb\r\n```\r\n")
			check := func(want text.Range, source []byte) {
				context.Helper()
				input := state.codeBlock.input
				if input == nil || input.Selection() != (text.Range{Start: min(want.Start, want.End), End: max(want.Start, want.End)}) || input.Caret() != want.End {
					context.Errorf("directed selection did not match %+v", want)
				}
				expectRenderedCollisionSave(context, state, driver, path, source)
			}
			check(after, outdented)
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			check(before, originalBytes)
			if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
				context.Fatal(err)
			}
			check(after, outdented)
		})
	}
}

func TestRenderedFencedCodeReverseIndentKeepsEarlierUndo(test *testing.T) {
	tail := strings.Repeat("untouched line\n", 12_000)
	const opening = "```go\n"
	const closing = "```\n"
	original := opening + "ab\ncd\n" + tail + closing
	state := model{session: session.New(), mode: renderedMode}
	current := state.session.Current()
	current.Editor = text.NewEditor(original)
	if err := current.EditRange(len(opening), len(opening), "X"); err != nil {
		test.Fatal(err)
	}
	prior := current.Text()
	block := markdownedit.Parse([]byte(prior))[0]
	if !block.Code.Editable {
		test.Fatal("long fenced code is not editable")
	}
	state.codeBlock = codeBlockEditor{
		active: true, documentID: current.ID, source: prior, block: block, input: text.NewEditor(block.Code.Visible),
	}
	state.codeBlock.input.SetSelection(text.Range{Start: 6, End: 1})
	state.indentFencedCode(false)
	if current.Text() != opening+"\tXab\n\tcd\n"+tail+closing {
		test.Fatal("reverse indent did not edit exactly the two selected lines")
	}
	state.stepCodeBlockHistory(app.TextUndo)
	if current.Text() != prior || state.codeBlock.input.Caret() != 1 {
		test.Fatal("first Undo did not restore the directed selection and text before indentation")
	}
	state.stepCodeBlockHistory(app.TextUndo)
	if current.Text() != original {
		test.Fatal("indentation discarded the prior Undo entry")
	}
}

func TestRenderedFencedCodeIndentCanUndoLongSelectedLine(test *testing.T) {
	const opening = "```go\n"
	const closing = "```\n"
	line := strings.Repeat("x", 2*1024*1024+1)
	original := opening + line + "\n" + closing
	state := model{session: session.New(), mode: renderedMode}
	current := state.session.Current()
	current.Editor = text.NewEditor(original)
	block := markdownedit.Parse([]byte(original))[0]
	if !block.Code.Editable {
		test.Fatal("long fenced code is not editable")
	}
	state.codeBlock = codeBlockEditor{
		active: true, documentID: current.ID, source: original, block: block, input: text.NewEditor(block.Code.Visible),
	}
	state.codeBlock.input.SetSelection(text.Range{Start: 0, End: len(line)})
	state.indentFencedCode(false)
	if current.Text() != opening+"\t"+line+"\n"+closing {
		test.Fatal("indent did not insert a single tab")
	}
	state.stepCodeBlockHistory(app.TextUndo)
	if current.Text() != original || state.codeBlock.input.Selection() != (text.Range{Start: 0, End: len(line)}) {
		test.Fatal("indentation of a long selected line could not be undone")
	}
}

func TestRenderedFencedCodeOutdentKeepsBlankBodyLine(test *testing.T) {
	state, driver, path, original := renderedSourceCollisionFixture(test, "```go\n\n```\n", false, 1200)
	clickID(test, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		test.Fatal(err)
	}
	indented := []byte("\xef\xbb\xbf```go\r\n\t\r\n```\r\n")
	expectRenderedCollisionSave(test, state, driver, path, indented)
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		test.Fatal(err)
	}
	expectRenderedCollisionSave(test, state, driver, path, original)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		test.Fatal(err)
	}
	expectRenderedCollisionSave(test, state, driver, path, indented)
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		test.Fatal(err)
	}
	expectRenderedCollisionSave(test, state, driver, path, original)
}
