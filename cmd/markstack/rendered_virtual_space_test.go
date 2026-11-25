package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
)

func TestRenderedVirtualParagraphKeepsFirstSpace(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1605} {
			for _, crlf := range []bool{false, true} {
				test.Run(fmt.Sprintf("dark=%v/width=%d/crlf=%v", dark, width, crlf), func(test *testing.T) {
					const tail = "Tail\n\n<!-- opaque -->\n"
					encode := func(body string) string {
						if crlf {
							return "\xef\xbb\xbf" + strings.ReplaceAll(body, "\n", "\r\n")
						}
						return body
					}
					source := func(value string) string { return "Body\n\n" + value + "\n\n" + tail }
					filename := filepath.Join(test.TempDir(), "paragraph.md")
					original := []byte(encode("Body\n\n" + tail))
					if err := os.WriteFile(filename, original, 0640); err != nil {
						test.Fatal(err)
					}
					state, driver := testDriver(test)
					state.dark = dark
					if !state.openFile(filename) {
						test.Fatal(state.message)
					}
					if err := driver.Resize(width, 780); err != nil {
						test.Fatal(err)
					}
					clickID(test, driver, renderedModeID)
					clickID(test, driver, renderedRows.At(0))
					for _, key := range []gfx.Key{gfx.KeyEnd, gfx.KeyEnter} {
						if err := driver.Key(key, 0); err != nil {
							test.Fatal(err)
						}
					}
					input := state.rich.input
					for _, key := range []string{"N", "e", "x", "t"} {
						if err := driver.Type(key); err != nil {
							test.Fatal(err)
						}
					}
					tailTop := state.preview.blocks[2].top
					check := func(value string) {
						test.Helper()
						caret := len(value)
						body := strings.TrimPrefix(encode(source(value)), "\xef\xbb\xbf")
						if state.session.Current().Text() != body || state.mode != renderedMode || !state.rich.active || state.rich.input != input ||
							input.String() != value || input.Selection() != (text.Range{Start: caret, End: caret}) || driver.Snapshot().Focused != richInputID {
							test.Fatalf("lost active paragraph for %q: mode=%v, source=%q, message=%q", value, state.mode, state.session.Current().Text(), state.message)
						}
						if len(state.preview.blocks) < 3 || state.preview.blocks[2].top != tailTop {
							test.Fatal("editing moved the following paragraph")
						}
					}
					for _, step := range []struct{ key, want string }{{" ", "Next "}, {"c", "Next c"}} {
						input.BreakUndoGroup()
						if err := driver.Type(step.key); err != nil {
							test.Fatal(err)
						}
						check(step.want)
					}
					if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, original) {
						test.Fatalf("typing changed the original before Save: %v", err)
					}
					expectRenderedCollisionSave(test, state, driver, filename, []byte(encode(source("Next c"))))
					for _, step := range []struct {
						key   rune
						value string
					}{{'z', "Next "}, {'z', "Next"}, {'y', "Next "}, {'y', "Next c"}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.key); err != nil {
							test.Fatal(err)
						}
						check(step.value)
						expectRenderedCollisionSave(test, state, driver, filename, []byte(encode(source(step.value))))
					}
				})
			}
		}
	}
}

func TestRenderedVirtualParagraphSpaceKeepsUnsafeSourceFallback(test *testing.T) {
	for _, value := range []string{" ", " Next ", "- Next ", "# Next ", "Next  ", "Next  \nline", "`Next` "} {
		test.Run(value, func(test *testing.T) {
			rich := renderedEditor{
				block: markdownedit.Block{Kind: markdownedit.Paragraph}, source: "Body\n\n\n\nTail\n",
				virtualStart: len("Body\n\n"), virtualEnd: len("Body\n\n"),
			}
			if next, err := virtualBlockSource(&rich, value); next != nil || !errors.Is(err, markdownedit.ErrReadOnlyBlock) {
				test.Fatalf("unsafe value %q: got %q, %v; want Source fallback", value, next, err)
			}
		})
	}
}
