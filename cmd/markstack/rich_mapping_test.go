package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

var renderedSourceCollisionCases = []struct {
	name        string
	source      string
	edited      string
	editableRow int
}{
	{"padded-inline-code", "` same `\n\nsame\n\nTail\n", "` same `\n\nsameX\n\nTail\n", 1},
	{"formatted-list", "- **same**\n- same\n\nTail\n", "- **same**\n- sameX\n\nTail\n", 1},
	{"code-heading", "# `same`\n\n## same\n\nTail\n", "# `same`\n\n## sameX\n\nTail\n", 1},
	{"code-quote", "> `same`\n\nBridge\n\n> same\n\nTail\n", "> `same`\n\nBridge\n\n> sameX\n\nTail\n", 2},
}

func renderedSourceCollisionFixture(context *testing.T, source string, dark bool, width int) (*model, *app.Driver, string, []byte) {
	context.Helper()
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(source, "\n", "\r\n"))...)
	filename := filepath.Join(context.TempDir(), "collision.md")
	if err := os.WriteFile(filename, original, 0640); err != nil {
		context.Fatal(err)
	}
	if err := os.Chmod(filename, 0640); err != nil {
		context.Fatal(err)
	}
	state, driver := testDriver(context)
	state.dark = dark
	state.openFile(filename)
	if err := driver.Resize(width, 780); err != nil {
		context.Fatal(err)
	}
	clickID(context, driver, renderedModeID)
	return state, driver, filename, original
}

func expectRenderedCollisionSave(context *testing.T, state *model, driver *app.Driver, filename string, expected []byte) {
	context.Helper()
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		context.Fatal(err)
	}
	actual, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(actual, expected) {
		context.Errorf("saved collision source=%q, want %q, error=%v", actual, expected, err)
	}
	if state.session.Current().Dirty() {
		context.Error("save left the collision document dirty")
	}
	if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0640 {
		context.Errorf("collision source permissions changed: %v, %v", info, err)
	}
}

func TestRenderedReadOnlyCollisionCannotWriteAnotherSourceInterval(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			for _, scenario := range renderedSourceCollisionCases {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, dark, width)
					clickID(context, driver, renderedRows.At(0))
					if state.mode != editMode || driver.Snapshot().Focused != sourceID || state.rich.active {
						context.Errorf("read-only block activated another source interval: mode=%v rich=%v source-index=%d", state.mode, state.rich.active, state.rich.sourceIndex)
					}
					if state.session.Current().Dirty() {
						context.Error("read-only activation changed dirty state")
					}
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, append([]byte("X"), original[3:]...)...)
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedEditableCollisionKeepsItsOwnSourceInterval(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			for _, scenario := range renderedSourceCollisionCases {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, dark, width)
					clickID(context, driver, renderedRows.At(scenario.editableRow))
					if state.mode != renderedMode || !state.rich.active || state.rich.index != scenario.editableRow {
						context.Fatalf("editable collision lost its source mapping: mode=%v rich=%v index=%d", state.mode, state.rich.active, state.rich.index)
					}
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.edited, "\n", "\r\n"))...)
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedFallbackStartsAtClickedSourceLine(test *testing.T) {
	const source = "# \u6807\u9898\n\n> first\n>\n> second\n\nTail\n"
	const edited = "# \u6807\u9898\n\n> first\n>\nX> second\n\nTail\n"
	prefix := strings.ReplaceAll("# \u6807\u9898\n\n> first\n>\n", "\n", "\r\n")
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
				clickID(context, driver, editModeID)
				clickID(context, driver, sourceID)
				end := state.session.Current().Editor.RuneCount()
				if err := driver.SelectText(end, end); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, renderedModeID)
				clickID(context, driver, renderedRows.At(2))
				if state.mode != editMode || driver.Snapshot().Focused != sourceID || state.rich.active {
					context.Fatal("multi-paragraph quote did not retain its explicit source fallback")
				}
				selection := state.session.Current().Editor.Selection()
				wantedOffset := utf8.RuneCountInString(prefix)
				if selection.Start != wantedOffset || selection.End != wantedOffset {
					context.Errorf("fallback selection=%v, want clicked line offset %d", selection, wantedOffset)
				}
				if err := driver.Type("X"); err != nil {
					context.Fatal(err)
				}
				wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(edited, "\n", "\r\n"))...)
				expectRenderedCollisionSave(context, state, driver, filename, wanted)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}
