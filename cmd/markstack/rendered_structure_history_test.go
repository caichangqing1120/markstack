package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedEnterAndBackspaceKeepStructuralHistory(test *testing.T) {
	for _, scenario := range []struct {
		name         string
		body         string
		key          gfx.Key
		caret        int
		after        string
		afterVisible string
		afterCaret   int
		continued    string
	}{
		{"split-plain", "abcd", gfx.KeyEnter, 2, "ab\n\ncd", "cd", 0, "ab\n\nQcd"},
		{"split-bold", "**abcd**", gfx.KeyEnter, 2, "**ab**\n\n**cd**", "cd", 0, "**ab**\n\n**Qcd**"},
		{"split-italic", "_abcd_", gfx.KeyEnter, 2, "_ab_\n\n_cd_", "cd", 0, "_ab_\n\n_Qcd_"},
		{"split-strike", "~~abcd~~", gfx.KeyEnter, 2, "~~ab~~\n\n~~cd~~", "cd", 0, "~~ab~~\n\n~~Qcd~~"},
		{"split-heading", "## left **abcd** right", gfx.KeyEnter, 7, "## left **ab**\n\n**cd** right", "cd right", 0, "## left **ab**\n\n**Qcd** right"},
		{"split-adjacent-styles", "**ab**_cd_", gfx.KeyEnter, 2, "**ab**\n\n_cd_", "cd", 0, "**ab**\n\n_Qcd_"},
		{"split-unicode", "\u4e2d\u6587\U0001f469\u200d\U0001f4bb e\u0301Z", gfx.KeyEnter, 2, "\u4e2d\u6587\n\n\U0001f469\u200d\U0001f4bb e\u0301Z", "\U0001f469\u200d\U0001f4bb e\u0301Z", 0, "\u4e2d\u6587\n\nQ\U0001f469\u200d\U0001f4bb e\u0301Z"},
		{"merge-plain", "Left\n\nRight", gfx.KeyBackspace, 0, "LeftRight", "LeftRight", 4, "LeftQRight"},
		{"merge-heading", "### Left\n\nRight", gfx.KeyBackspace, 0, "### LeftRight", "LeftRight", 4, "### LeftQRight"},
		{"merge-unicode", "\u4e2d\u6587\n\n\U0001f469\u200d\U0001f4bb e\u0301Z", gfx.KeyBackspace, 0, "\u4e2d\u6587\U0001f469\u200d\U0001f4bb e\u0301Z", "\u4e2d\u6587\U0001f469\u200d\U0001f4bb e\u0301Z", 2, "\u4e2d\u6587Q\U0001f469\u200d\U0001f4bb e\u0301Z"},
		{"merge-long-suffix", "Left\n\n" + strings.Repeat("keep ", 30) + "end", gfx.KeyBackspace, 0, "Left" + strings.Repeat("keep ", 30) + "end", "Left" + strings.Repeat("keep ", 30) + "end", 4, "LeftQ" + strings.Repeat("keep ", 30) + "end"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.body+suffix, dark, width)
					beforeRow, afterRow := 1, 2
					if scenario.key == gfx.KeyBackspace {
						beforeRow, afterRow = 2, 1
					}
					clickID(context, driver, renderedRows.At(beforeRow))
					if state.mode != renderedMode || !state.rich.active {
						context.Fatal("supported structural fixture did not activate")
					}
					beforeVisible := state.rich.input.String()
					state.rich.input.MoveTo(scenario.caret, false)
					if err := driver.Key(scenario.key, 0); err != nil {
						context.Fatal(err)
					}
					merged := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+scenario.after+suffix, "\n", "\r\n"))...)
					continued := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+scenario.continued+suffix, "\n", "\r\n"))...)
					assertPosition := func(row, caret int, visible string) {
						context.Helper()
						if state.mode != renderedMode || !state.rich.active || state.rich.index != row || state.rich.input.String() != visible || state.rich.input.Selection() != (text.Range{Start: caret, End: caret}) || driver.Snapshot().Focused != richInputID {
							context.Fatalf("structural history lost its rendered position: mode=%v active=%v row=%d, want row=%d caret=%d visible=%q", state.mode, state.rich.active, state.rich.index, row, caret, visible)
						}
					}
					assertPosition(afterRow, scenario.afterCaret, scenario.afterVisible)
					expectRenderedCollisionSave(context, state, driver, filename, merged)
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
					visibleOffset := byteOffsetOfRune(scenario.afterVisible, scenario.afterCaret)
					continuedVisible := scenario.afterVisible[:visibleOffset] + "Q" + scenario.afterVisible[visibleOffset:]
					assertPosition(afterRow, scenario.afterCaret+1, continuedVisible)
					expectRenderedCollisionSave(context, state, driver, filename, continued)
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							bytes   []byte
							row     int
							caret   int
							visible string
						}{
							{'z', merged, afterRow, scenario.afterCaret, scenario.afterVisible},
							{'z', original, beforeRow, scenario.caret, beforeVisible},
							{'y', merged, afterRow, scenario.afterCaret, scenario.afterVisible},
							{'y', continued, afterRow, scenario.afterCaret + 1, continuedVisible},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							assertPosition(step.row, step.caret, step.visible)
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					}
					if err := driver.Type("R"); err != nil {
						context.Fatal(err)
					}
					latest := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+strings.Replace(scenario.continued, "Q", "QR", 1)+suffix, "\n", "\r\n"))...)
					expectRenderedCollisionSave(context, state, driver, filename, latest)
					for _, wanted := range [][]byte{continued, merged, original} {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, wanted)
					}
					assertPosition(beforeRow, scenario.caret, beforeVisible)
					if state.session.Current().Editor.CanUndo() {
						context.Fatal("structural history invented additional source transactions")
					}
					if scenario.name == "split-plain" || scenario.name == "merge-plain" {
						state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
						if err := driver.Type("N"); err != nil {
							context.Fatal(err)
						}
						branch := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+scenario.body+"N"+suffix, "\n", "\r\n"))...)
						for _, step := range []struct {
							command rune
							bytes   []byte
						}{{0, branch}, {'z', original}, {'y', branch}, {'y', branch}} {
							if step.command != 0 {
								if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
									context.Fatal(err)
								}
							}
							if state.mode != renderedMode || !state.rich.active {
								context.Fatal("new typing restored an obsolete structural redo")
							}
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					}
				})
			}
		}
	}
}

func TestRenderedStructuralHistoryDoesNotInterceptEarlierRepeatedSource(test *testing.T) {
	for _, scenario := range []struct {
		name  string
		body  string
		key   gfx.Key
		row   int
		caret int
		after string
	}{
		{"split", "abcd", gfx.KeyEnter, 1, 2, "ab\n\ncd"},
		{"merge", "Left\n\nRight", gfx.KeyBackspace, 2, 0, "LeftRight"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.body+suffix, dark, width)
					clickID(context, driver, renderedRows.At(scenario.row))
					if err := driver.Type("P"); err != nil {
						context.Fatal(err)
					}
					if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						context.Fatal(err)
					}
					state.rich.input.MoveTo(scenario.caret, false)
					if err := driver.Key(scenario.key, 0); err != nil {
						context.Fatal(err)
					}
					previous := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+scenario.body+"P"+suffix, "\n", "\r\n"))...)
					after := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+scenario.after+suffix, "\n", "\r\n"))...)
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							bytes   []byte
						}{{'z', original}, {'z', previous}, {'z', original}, {'y', previous}, {'y', original}, {'y', after}} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active {
								context.Fatalf("same-source structural history changed mode during cycle %d/%c: mode=%v", cycle, step.command, state.mode)
							}
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					}
				})
			}
		}
	}
}

func TestRenderedStructuralHistorySurvivesFormatRebasing(test *testing.T) {
	for _, scenario := range []struct {
		name       string
		body       string
		key        gfx.Key
		row        int
		caret      int
		after      string
		typed      string
		formatted  string
		afterRow   int
		afterCaret int
	}{
		{"split", "abcd", gfx.KeyEnter, 1, 2, "ab\n\ncd", "ab\n\nQcd", "ab\n\n**Qcd**", 2, 0},
		{"merge", "Left\n\nRight", gfx.KeyBackspace, 2, 0, "LeftRight", "LeftQRight", "**LeftQRight**", 1, 4},
		{"paste", "# Title\n\nBody", gfx.KeyDown, 1, 5, "# Title plus", "# Title plusQ", "## Title plusQ", 1, 10},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.body+suffix, dark, width)
					clickID(context, driver, renderedRows.At(scenario.row))
					state.rich.input.MoveTo(scenario.caret, false)
					modifier := gfx.Modifiers(0)
					if scenario.name == "paste" {
						modifier = gfx.ModifierShift
					}
					if err := driver.Key(scenario.key, modifier); err != nil {
						context.Fatal(err)
					}
					if scenario.name == "paste" {
						driver.SetClipboard(" plus")
						if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
							context.Fatal(err)
						}
					}
					bytesFor := func(body string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body+suffix, "\n", "\r\n"))...)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(scenario.after))
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(scenario.typed))
					if scenario.name == "paste" {
						state.applyHeadingLevel(2)
					} else {
						state.applyBoldBlock()
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(scenario.formatted))
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							bytes   []byte
							row     int
							caret   int
						}{
							{'z', bytesFor(scenario.typed), scenario.afterRow, scenario.afterCaret + 1},
							{'z', bytesFor(scenario.after), scenario.afterRow, scenario.afterCaret},
							{'z', original, scenario.row, scenario.caret},
							{'y', bytesFor(scenario.after), scenario.afterRow, scenario.afterCaret},
							{'y', bytesFor(scenario.typed), scenario.afterRow, scenario.afterCaret + 1},
							{'y', bytesFor(scenario.formatted), scenario.afterRow, scenario.afterCaret + 1},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != step.row || state.rich.input.Caret() != step.caret || driver.Snapshot().Focused != richInputID {
								context.Fatalf("format rebasing lost structural history at cycle %d/%c: mode=%v active=%v row=%d, want row=%d caret=%d", cycle, step.command, state.mode, state.rich.active, state.rich.index, step.row, step.caret)
							}
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					}
				})
			}
		}
	}
}

func TestRenderedStructuralRedoIsDiscardedByNewFormattedBranch(test *testing.T) {
	for _, scenario := range []struct {
		name      string
		body      string
		key       gfx.Key
		row       int
		caret     int
		typed     string
		formatted string
	}{
		{"split", "abcd", gfx.KeyEnter, 1, 2, "abNcd", "**abNcd**"},
		{"merge", "Left\n\nRight", gfx.KeyBackspace, 2, 0, "Left\n\nNRight", "Left\n\n**NRight**"},
		{"paste", "# Title\n\nBody", gfx.KeyDown, 1, 5, "# TitleN\n\nBody", "## TitleN\n\nBody"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.body+suffix, dark, width)
					clickID(context, driver, renderedRows.At(scenario.row))
					state.rich.input.MoveTo(scenario.caret, false)
					modifier := gfx.Modifiers(0)
					if scenario.name == "paste" {
						modifier = gfx.ModifierShift
					}
					if err := driver.Key(scenario.key, modifier); err != nil {
						context.Fatal(err)
					}
					if scenario.name == "paste" {
						driver.SetClipboard(" plus")
						if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
							context.Fatal(err)
						}
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if err := driver.Type("N"); err != nil {
						context.Fatal(err)
					}
					if scenario.name == "paste" {
						state.applyHeadingLevel(2)
					} else {
						state.applyBoldBlock()
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					bytesFor := func(body string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body+suffix, "\n", "\r\n"))...)
					}
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							bytes   []byte
							caret   int
						}{{'z', bytesFor(scenario.typed), scenario.caret + 1}, {'z', original, scenario.caret}, {'y', bytesFor(scenario.typed), scenario.caret + 1}, {'y', bytesFor(scenario.formatted), scenario.caret + 1}} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != scenario.row || state.rich.input.Selection() != (text.Range{Start: step.caret, End: step.caret}) || driver.Snapshot().Focused != richInputID {
								context.Fatal("obsolete structural redo intercepted a committed new branch")
							}
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					}
					if state.session.Current().Editor.CanRedo() {
						context.Fatal("obsolete structural redo survived the new branch")
					}
				})
			}
		}
	}
}

func TestRenderedStructuralRedoSurvivesCompositionCancel(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n", dark, width)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				for _, event := range []app.TextEvent{{Kind: app.TextSetMarked, Text: "pin"}, {Kind: app.TextCancelComposition}} {
					if _, err := state.onRenderedTextEvent(event); err != nil {
						context.Fatal(err)
					}
					state.syncRendered(state.session.Current())
				}
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.Caret() != 0 {
					context.Fatal("cancelled composition discarded an otherwise valid structural redo")
				}
				wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte("ab\r\n\r\ncd\r\n")...)
				expectRenderedCollisionSave(context, state, driver, filename, wanted)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				state.applyBoldBlock()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				if len(state.rich.structureRedo) != 0 {
					context.Fatal("committed formatting retained an obsolete structural redo")
				}
				formatted := append([]byte{0xef, 0xbb, 0xbf}, []byte("**abcd**\r\n")...)
				for _, step := range []struct {
					command rune
					bytes   []byte
				}{{0, formatted}, {'z', original}, {'y', formatted}} {
					if step.command != 0 {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
					}
					expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
				}
			})
		}
	}
}
