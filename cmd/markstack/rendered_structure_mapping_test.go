package main

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/markdown"

	"markstack/internal/markdownedit"
)

func TestRenderedTableRefusesAmbiguousEscapedSeparatorBesideComment(test *testing.T) {
	const source = "Lead.\n<!-- opaque -->\n\n| A | B | C |\n| --- | --- | --- |\n| a\\\\| same | same | tail |\n"
	state, driver, path, original := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedTableCells.At(4))
	if state.mode != editMode || state.table.active {
		if err := driver.Type("X"); err != nil {
			test.Fatal(err)
		}
		test.Fatalf("ambiguous table cell was made editable; source became %q", state.session.Current().Text())
	}
	if caret := state.session.Current().Editor.Caret(); caret != len("Lead.\r\n<!-- opaque -->\r\n\r\n") {
		test.Fatalf("unverified cell moved Source caret away from the table start: %d", caret)
	}
	expectRenderedCollisionSave(test, state, driver, path, original)
}

func TestRenderedTablePaddedFallbackRejectsUnverifiedNeighbor(test *testing.T) {
	const prefix = "Lead.\n<!-- opaque -->\n\n"
	const source = prefix + "| A | B | C | D |\n| --- | --- | --- | --- |\n| a\\\\| first \\\\| second | tail |\n"
	state, driver, path, original := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedTableCells.At(6))
	if state.mode != editMode || state.table.active {
		test.Fatal("ambiguous synthetic cell did not fall back to Source")
	}
	if caret := state.session.Current().Editor.Caret(); caret != len(strings.ReplaceAll(prefix, "\n", "\r\n")) {
		test.Fatalf("synthetic cell used an unverified anchor: caret=%d", caret)
	}
	expectRenderedCollisionSave(test, state, driver, path, original)
}

func TestRenderedHeaderOnlyTableRetainsExactSourceOwner(test *testing.T) {
	for _, fixture := range []struct{ name, prefix string }{
		{"standalone", ""},
		{"beside-comment", "Lead.\n<!-- opaque -->\n\n"},
	} {
		test.Run(fixture.name, func(context *testing.T) {
			const header = "| A | B |\n| :--- | ---: |"
			state, driver, path, _ := renderedSourceCollisionFixture(context, fixture.prefix+header, false, 1200)
			clickID(context, driver, renderedTableCells.At(0))
			if state.mode != renderedMode || !state.table.active {
				context.Fatal("header-only table did not activate its first cell")
			}
			if err := driver.Type("X"); err != nil {
				context.Fatal(err)
			}
			want := []byte("\xef\xbb\xbf" + strings.ReplaceAll(fixture.prefix+"| AX | B |\n| :--- | ---: |", "\n", "\r\n"))
			expectRenderedCollisionSave(context, state, driver, path, want)
		})
	}
}

func TestRenderedTableRetainsPhysicalRowsBeforePaddedLastRow(test *testing.T) {
	const source = "| A | B |\n| --- | --- |\n| left | right |\n|\n"
	state, driver, path, _ := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedTableCells.At(0))
	if state.mode != renderedMode || !state.table.active {
		test.Fatal("header did not activate before an all-padded final row")
	}
	clickID(test, driver, renderedTableCells.At(2))
	if state.mode != renderedMode || !state.table.active || state.table.row != 1 || state.table.column != 0 {
		test.Fatal("physical body cell did not activate before an all-padded final row")
	}
	if err := driver.Type("X"); err != nil {
		test.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf" + strings.ReplaceAll(strings.Replace(source, "left", "leftX", 1), "\n", "\r\n"))
	expectRenderedCollisionSave(test, state, driver, path, want)
	for _, hit := range []int{4, 5} {
		clickID(test, driver, renderedTableCells.At(hit))
		if state.mode != editMode || state.table.active {
			test.Fatalf("synthetic cell %d must fall back to Source", hit)
		}
		expectRenderedCollisionSave(test, state, driver, path, want)
		clickID(test, driver, renderedModeID)
	}
}

func TestRenderedTableRetainsPhysicalRowsAfterPaddedHeader(test *testing.T) {
	const source = "|\n| --- | --- |\n| left | right |\n"
	state, driver, path, original := renderedSourceCollisionFixture(test, source, false, 1200)
	for _, hit := range []int{0, 1} {
		clickID(test, driver, renderedTableCells.At(hit))
		if state.mode != editMode || state.table.active {
			test.Fatalf("synthetic header cell %d must fall back to Source", hit)
		}
		expectRenderedCollisionSave(test, state, driver, path, original)
		clickID(test, driver, renderedModeID)
	}
	clickID(test, driver, renderedTableCells.At(3))
	if state.mode != renderedMode || !state.table.active || state.table.row != 1 || state.table.column != 1 {
		test.Fatal("physical body cell did not activate after an all-padded header")
	}
	if err := driver.Type("X"); err != nil {
		test.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf" + strings.ReplaceAll(strings.Replace(source, "right", "rightX", 1), "\n", "\r\n"))
	expectRenderedCollisionSave(test, state, driver, path, want)
}

func TestRenderedFenceRetainsOwnerBesideMultilineCommentAndRule(test *testing.T) {
	const fence = "```go\nkeep\n```\n"
	for _, fixture := range []struct{ name, prefix string }{
		{"multiline-comment", "Lead.\n<!-- opaque\ncomment end -->\n\n"},
		{"with-rule", "Lead.\n<!-- opaque -->\n\n***\n\n"},
	} {
		test.Run(fixture.name, func(context *testing.T) {
			prefix := fixture.prefix + fence + "\nBridge.\n\n"
			state, driver, path, _ := renderedSourceCollisionFixture(context, prefix+fence, false, 1200)
			selected := -1
			for index, block := range state.parsed.Blocks {
				if block.Kind == markdown.Code {
					selected = index
				}
			}
			if selected < 0 {
				context.Fatal("rendered fence missing")
			}
			clickID(context, driver, renderedRows.At(selected))
			if state.mode != renderedMode || !state.codeBlock.active || state.codeBlock.renderedIndex != selected {
				context.Fatal("second rendered fence did not activate")
			}
			if err := driver.Type("X"); err != nil {
				context.Fatal(err)
			}
			want := []byte("\xef\xbb\xbf" + strings.ReplaceAll(prefix+"```go\nkeepX\n```\n", "\n", "\r\n"))
			expectRenderedCollisionSave(context, state, driver, path, want)
		})
	}
}

func TestRenderedSpecialBlockMappingAcrossOpaqueComments(test *testing.T) {
	for _, fixture := range []struct {
		name, before, after string
		kind                markdown.Kind
	}{
		{"table", "| Left | Center | Right |\n| :--- | :---: | ---: |\n| keep |  | edge |\n", "| Left | Center | Right |\n| :--- | :---: | ---: |\n| keep |  X| edge |\n", markdown.Table},
		{"fence", "```go\none\ntwo\n```\n", "```go\none\ntwoX\n```\n", markdown.Code},
		{"empty-fence", "~~~go\n~~~\n", "~~~go\nX\n~~~\n", markdown.Code},
	} {
		for _, placement := range []struct{ name, prefix, suffix string }{
			{"before", "Opening.\n<!-- before -->\n\n", "\nTail.\n"},
			{"after", "", "\nTail.\n<!-- after -->\n"},
			{"both", "Opening.\n<!-- before -->\n\n", "\nTail.\n<!-- after -->\n"},
		} {
			test.Run(fixture.name+"/"+placement.name, func(context *testing.T) {
				prefix := placement.prefix + fixture.before + "\nBridge.\n\n"
				state, driver, path, original := renderedSourceCollisionFixture(context, prefix+fixture.before+placement.suffix, false, 1200)
				if len(state.parsed.Blocks) == len(markdownedit.Parse([]byte(state.session.Current().Text()))) {
					context.Fatal("fixture no longer exercises parser disagreement")
				}
				selected, priorCells := -1, 0
				for index, block := range state.parsed.Blocks {
					if block.Kind != fixture.kind {
						continue
					}
					if selected >= 0 {
						selected = index
						break
					}
					selected = index
					if fixture.kind == markdown.Table {
						priorCells = len(state.parsed.Cells(&block))
					}
				}
				if selected < 0 {
					context.Fatal("rendered block missing")
				}
				checkOwner := func() {
					context.Helper()
					if state.mode != renderedMode || fixture.kind == markdown.Table && (!state.table.active || state.table.renderedIndex != selected || state.table.row != 1 || state.table.column != 1) || fixture.kind == markdown.Code && (!state.codeBlock.active || state.codeBlock.renderedIndex != selected) {
						context.Fatalf("second rendered block did not own the edit: %s", state.message)
					}
				}
				if fixture.kind == markdown.Table {
					clickID(context, driver, renderedTableCells.At(priorCells+4))
				} else {
					clickID(context, driver, renderedRows.At(selected))
				}
				checkOwner()
				if err := driver.Type("X"); err != nil {
					context.Fatal(err)
				}
				want := []byte("\xef\xbb\xbf" + strings.ReplaceAll(prefix+fixture.after+placement.suffix, "\n", "\r\n"))
				expectRenderedCollisionSave(context, state, driver, path, want)
				for _, navigate := range []bool{false, true} {
					for _, entry := range []struct {
						key  rune
						data []byte
					}{{'z', original}, {'y', want}} {
						if navigate {
							clickID(context, driver, readModeID)
							clickID(context, driver, renderedModeID)
						}
						if _, err := driver.ShortcutChord(primaryModifier(), entry.key); err != nil {
							context.Fatal(err)
						}
						checkOwner()
						expectRenderedCollisionSave(context, state, driver, path, entry.data)
					}
				}
			})
		}
	}
}
