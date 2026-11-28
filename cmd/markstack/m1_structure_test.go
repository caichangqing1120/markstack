package main

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
)

func m1Shortcut(test *testing.T, driver *app.Driver, modifiers gfx.Modifiers, key rune) {
	test.Helper()
	if handled, err := driver.ShortcutChord(modifiers, key); err != nil || !handled {
		test.Fatalf("shortcut %q handled=%v error=%v", key, handled, err)
	}
}

func m1Saved(value string) []byte {
	return []byte("\xef\xbb\xbf" + strings.ReplaceAll(value, "\n", "\r\n"))
}

func TestM1TableToolbarAndShortcutsRoundTrip(test *testing.T) {
	const header = "| H | J |\n| --- | --- |\n"
	const first = "| one | 甲 |\n"
	const last = "| two | 乙 |\n"
	const blank = "|   |   |\n"
	const trailing = "\nTail **stays**.\n"
	for _, entry := range []struct {
		action   markdownedit.TableAction
		key      rune
		want     string
		row, col int
	}{
		{markdownedit.RowAbove, 0, header + blank + first + last + trailing, 1, 1},
		{markdownedit.RowBelow, 'j', header + first + blank + last + trailing, 2, 1},
		{markdownedit.RowDelete, 0, header + last + trailing, 1, 1},
		{markdownedit.ColumnLeft, 'h', "| H |   | J |\n| --- | --- | --- |\n| one |   | 甲 |\n| two |   | 乙 |\n" + trailing, 1, 1},
		{markdownedit.ColumnRight, 0, "| H | J |   |\n| --- | --- | --- |\n| one | 甲 |   |\n| two | 乙 |   |\n" + trailing, 1, 2},
		{markdownedit.ColumnDelete, 'k', "| H |\n| --- |\n| one |\n| two |\n" + trailing, 1, 0},
		{markdownedit.AlignLeft, 0, "| H | J |\n| --- | :--- |\n" + first + last + trailing, 1, 1},
		{markdownedit.AlignCenter, '2', "| H | J |\n| --- | :---: |\n" + first + last + trailing, 1, 1},
		{markdownedit.AlignRight, 0, "| H | J |\n| --- | ---: |\n" + first + last + trailing, 1, 1},
		{markdownedit.TableDelete, '0', trailing, -1, -1},
	} {
		test.Run(tableActionButtons[entry.action].label, func(test *testing.T) {
			state, driver, path, original := renderedSourceCollisionFixture(test, header+first+last+trailing, false, 1200)
			clickID(test, driver, renderedTableCells.At(3))
			if entry.key == 0 {
				clickID(test, driver, tableActions.At(int(entry.action)))
			} else {
				m1Shortcut(test, driver, primaryModifier()|gfx.ModifierShift, entry.key)
			}
			check := func() {
				test.Helper()
				if state.mode != renderedMode || (entry.row != -1 && (!state.table.active || state.table.row != entry.row || state.table.column != entry.col || driver.Snapshot().Focused != tableInputID)) || entry.row == -1 && !state.rich.active {
					test.Fatalf("action did not retain rendered target: mode=%v table=(%v %d,%d) focus=%v text=%q", state.mode, state.table.active, state.table.row, state.table.column, driver.Snapshot().Focused, state.session.Current().Text())
				}
				expectRenderedCollisionSave(test, state, driver, path, m1Saved(entry.want))
			}
			check()
			m1Shortcut(test, driver, primaryModifier(), 'z')
			if !state.table.active || state.table.row != 1 || state.table.column != 1 {
				test.Fatalf("undo did not restore original cell: %+v", state.table.cell)
			}
			expectRenderedCollisionSave(test, state, driver, path, original)
			m1Shortcut(test, driver, primaryModifier(), 'y')
			check()
		})
	}
}

func TestM1TableBoundaryAndComposition(test *testing.T) {
	state, driver, path, original := renderedSourceCollisionFixture(test, "|   |\n| --- |", false, 1200)
	clickID(test, driver, renderedTableCells.At(0))
	if err := driver.Compose("预"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, tableActions.At(int(markdownedit.ColumnDelete)))
	m1Shortcut(test, driver, primaryModifier()|gfx.ModifierShift, '0')
	if state.session.Current().Text() != string(original[3:]) || !state.tableComposing() || driver.Snapshot().Focused != tableInputID {
		test.Fatal("table operation changed source or displaced IME")
	}
	state.table.input.CancelComposition()
	clickID(test, driver, tableActions.At(int(markdownedit.ColumnDelete)))
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(""))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(test, state, driver, path, original)
	if !state.table.active || state.table.input.String() != "" {
		test.Fatal("undo failed to reconstruct blank header cell")
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		test.Fatal(err)
	}
	if !state.table.active || state.table.row != 1 || state.table.column != 0 {
		test.Fatal("Tab at the last cell did not add and focus a new row")
	}
}

func TestM1NestedTaskWorkflows(test *testing.T) {
	const source = "# Tasks\n\n- [ ] Parent\n- [ ] Child\n- [x] Other\n\nTail\n"
	const nested = "# Tasks\n\n- [ ] Parent\n  - [ ] Child\n- [x] Other\n\nTail\n"
	state, driver, path, original := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedRows.At(2))
	if err := driver.Compose("预"); err != nil {
		test.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, taskChecks.At(1))
	m1Shortcut(test, driver, primaryModifier()|gfx.ModifierShift, 't')
	if state.session.Current().Text() != string(original[3:]) || !state.richComposing() || driver.Snapshot().Focused != richInputID {
		test.Fatal("list or checkbox operation changed source or displaced IME")
	}
	state.rich.input.CancelComposition()
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		test.Fatal(err)
	}
	if !state.rich.active || state.parsed.Blocks[2].Level != 1 || state.rich.block.Visible != "Child" {
		test.Fatal("Tab did not keep the nested task active")
	}
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(nested))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(test, state, driver, path, original)
	m1Shortcut(test, driver, primaryModifier(), 'y')
	m1Shortcut(test, driver, primaryModifier()|gfx.ModifierShift, 't')
	checked := strings.Replace(nested, "[ ] Child", "[x] Child", 1)
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(checked))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(nested))
	m1Shortcut(test, driver, primaryModifier(), 'y')
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		test.Fatal(err)
	}
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(strings.Replace(source, "[ ] Child", "[x] Child", 1)))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(checked))
	clickID(test, driver, readModeID)
	clickID(test, driver, taskChecks.At(2))
	if state.mode != readMode || state.session.Current().Text() != string(m1Saved(nested)[3:]) {
		test.Fatal("read mode checkbox did not toggle in place")
	}
	m1Shortcut(test, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(checked))
	m1Shortcut(test, driver, primaryModifier(), 'y')
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(nested))
}

func TestM1EnterOnSavedEmptyListItem(test *testing.T) {
	for _, marker := range []string{"- ", "1. ", "- [ ] "} {
		test.Run(marker, func(test *testing.T) {
			state, driver, path, original := renderedSourceCollisionFixture(test, marker+"\n", false, 1200)
			clickID(test, driver, renderedRows.At(0))
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				test.Fatal(err)
			}
			if state.mode != renderedMode || !state.rich.virtual || state.rich.block.Kind != markdownedit.Paragraph {
				test.Fatal("Enter did not leave an editable ordinary paragraph")
			}
			expectRenderedCollisionSave(test, state, driver, path, m1Saved("\n"))
			m1Shortcut(test, driver, primaryModifier(), 'z')
			expectRenderedCollisionSave(test, state, driver, path, original)
			m1Shortcut(test, driver, primaryModifier(), 'y')
			if err := driver.Type("段落"); err != nil {
				test.Fatal(err)
			}
			expectRenderedCollisionSave(test, state, driver, path, m1Saved("段落\n"))
		})
	}
}

func TestM1FencedCodeLanguageCopyAndSelectAll(test *testing.T) {
	const source = "```go attrs\npackage main\n你好\n```\n\nTail\n"
	state, driver, path, original := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedRows.At(0))
	if err := driver.Compose("预"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, codeLanguageButtons.At(0))
	m1Shortcut(test, driver, primaryModifier(), 'a')
	if state.codeBlock.languageInput != nil || !state.codeBlockComposing() || state.mode != renderedMode || state.session.Current().Text() != string(original[3:]) {
		test.Fatal("code controls changed source or focus during IME")
	}
	state.codeBlock.input.CancelComposition()
	clickID(test, driver, codeCopyButtons.At(0))
	if got := driver.Clipboard(); got != "package main\r\n你好" {
		test.Fatalf("clipboard includes fences or omitted body: %q", got)
	}
	highlightColor := func() gfx.Color {
		test.Helper()
		frame := driver.Snapshot()
		for _, line := range frame.Texts {
			if line.Value == "package main" {
				for _, run := range frame.TextRuns(line) {
					if run.Column == 0 {
						return run.Color
					}
				}
				return line.Style.Color
			}
		}
		test.Error("code body line did not paint")
		return gfx.Color{}
	}
	beforeColor := highlightColor()
	clickID(test, driver, codeLanguageButtons.At(0))
	if err := driver.Compose("预"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, codeLanguageApplyID)
	if _, composing := state.codeBlock.languageInput.Composition(); !composing || state.session.Current().Text() != string(original[3:]) {
		test.Fatal("language Apply displaced composition")
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		test.Fatal(err)
	}
	if driver.Snapshot().Focused != codeLanguageInputID || state.session.Current().Text() != string(original[3:]) {
		test.Fatal("language field submitted preedit")
	}
	state.codeBlock.languageInput.CancelComposition()
	m1Shortcut(test, driver, primaryModifier(), 'a')
	if err := driver.Type("python"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, codeLanguageApplyID)
	if !state.codeBlock.active || state.codeBlock.block.Code.Info != "python attrs" || highlightColor() == beforeColor {
		test.Fatalf("language or highlight did not refresh: %q, message=%q", state.codeBlock.block.Code.Info, state.message)
	}
	expectRenderedCollisionSave(test, state, driver, path, m1Saved(strings.Replace(source, "go attrs", "python attrs", 1)))
	m1Shortcut(test, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(test, state, driver, path, original)
	m1Shortcut(test, driver, primaryModifier(), 'y')
	if !state.codeBlock.active || state.codeBlock.block.Code.Info != "python attrs" {
		test.Fatal("language history did not stay in the code editor")
	}
	m1Shortcut(test, driver, primaryModifier(), 'a')
	if state.mode != renderedMode || state.codeBlock.input.Selection() != (text.Range{End: state.codeBlock.input.RuneCount()}) {
		test.Fatal("first Select All did not cover the code only")
	}
	m1Shortcut(test, driver, primaryModifier(), 'a')
	editor := state.session.Current().Editor
	if state.mode != editMode || driver.Snapshot().Focused != sourceID || editor.Selection() != (text.Range{End: editor.RuneCount()}) {
		test.Fatal("second Select All did not cover the whole document")
	}
}
