package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestRenderedHeadingMenuChangesOnlySelectedBlock(t *testing.T) {
	for level := 1; level <= 6; level++ {
		t.Run(strings.Repeat("#", level), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "levels.md")
			source := []byte("\xef\xbb\xbf# First\r\n\r\nBody\r\n")
			if err := os.WriteFile(path, source, 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(1201, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(1))
			clickID(t, driver, headingMenuID)
			clickID(t, driver, headingLevels.At(level))
			want := "# First\r\n\r\n" + strings.Repeat("#", level) + " Body\r\n"
			if state.mode != renderedMode || state.session.Current().Text() != want {
				t.Fatalf("heading level %d: mode=%v source=%q", level, state.mode, state.session.Current().Text())
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "\xef\xbb\xbf"+want {
				t.Fatalf("saved heading = %q, %v", got, err)
			}
		})
	}
}

func TestRenderedHeadingMenuCanReturnToParagraphAndUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paragraph.md")
	if err := os.WriteFile(path, []byte("# Title\n\n## Body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, headingLevels.At(0))
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n" || state.mode != renderedMode {
		t.Fatalf("paragraph action = %q mode=%v", got, state.mode)
	}
	clickID(t, driver, richInputID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\n## Body\n" || state.session.Current().Dirty() {
		t.Fatalf("format undo = %q dirty=%v", got, state.session.Current().Dirty())
	}
}

func TestRenderedHeadingMenuDoesNotCoverActiveHeading(t *testing.T) {
	for _, width := range []int{740, 1200} {
		state, driver := testDriver(t)
		state.session.Current().Replace("# 墨栈\n\n正文\n")
		if err := driver.Resize(width, 780); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, renderedModeID)
		clickID(t, driver, renderedRows.At(0))
		clickID(t, driver, headingMenuID)
		var heading ui.Rect
		var measure gfx.Canvas
		for _, label := range driver.Snapshot().Texts {
			if label.Value == "墨栈" && label.Style.Size >= 30 {
				heading = label.Bounds
				heading.Width = measure.TextWidthWeight(label.Value, label.Style.Size, label.Style.Weight)
			}
		}
		if heading.Width == 0 {
			t.Fatal("active heading not painted")
		}
		for _, hit := range driver.Snapshot().Hits {
			if _, ok := hit.ID.In(headingLevels); ok && hit.Bounds.X < heading.X+heading.Width && hit.Bounds.X+hit.Bounds.Width > heading.X && hit.Bounds.Y < heading.Y+heading.Height && hit.Bounds.Y+hit.Bounds.Height > heading.Y {
				t.Fatalf("%dpx menu option covers heading: %+v vs %+v", width, hit.Bounds, heading)
			}
		}
	}
}

func TestRenderedHeadingMenuEscapeReturnsToEditor(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, headingMenuID)
	if !state.headingMenuOpen {
		t.Fatal("heading menu did not open")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.headingMenuOpen || driver.Snapshot().Focused != richInputID {
		t.Fatalf("Escape left heading menu open=%v focus=%v", state.headingMenuOpen, driver.Snapshot().Focused)
	}
}

func TestRenderedHeadingMenuClosesOnAnotherBlock(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, renderedRows.At(1))
	if state.headingMenuOpen || !state.rich.active || state.rich.index != 1 {
		t.Fatalf("menu remained after block navigation: open=%v row=%d", state.headingMenuOpen, state.rich.index)
	}
}

func TestRenderedHeadingMenuClosesOnBlankClick(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, headingMenuID)
	if err := driver.Click(650, 95); err != nil {
		t.Fatal(err)
	}
	if state.headingMenuOpen {
		t.Fatal("blank click left heading menu open")
	}
}

func TestRenderedWholeParagraphBoldTogglesWithoutRewritingOtherBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bold.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# Title\r\n\r\nBody\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, boldBlockID)
	if got := state.session.Current().Text(); got != "# Title\r\n\r\n**Body**\r\n\r\nTail\r\n" || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("bold action mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, richInputID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\r\n\r\nBody\r\n\r\nTail\r\n" {
		t.Fatalf("undo bold source=%q", got)
	}
	clickID(t, driver, boldBlockID)
	clickID(t, driver, boldBlockID)
	if got := state.session.Current().Text(); got != "# Title\r\n\r\nBody\r\n\r\nTail\r\n" || state.mode != renderedMode {
		t.Fatalf("toggle bold source=%q mode=%v", got, state.mode)
	}
	clickID(t, driver, boldBlockID)
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := "\xef\xbb\xbf# Title\r\n\r\n**Body**\r\n\r\nTail\r\n"
	if err != nil || string(got) != want {
		t.Fatalf("saved bold = %q, err=%v", got, err)
	}
}

func TestRenderedWholeParagraphItalicTogglesAndRemainsEditable(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, italicBlockID)
	if state.mode != renderedMode || state.session.Current().Text() != "# Title\n\n*Body*\n" {
		t.Fatalf("italic action mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\n*BodyX*\n" {
		t.Fatalf("italic input source=%q", got)
	}
	clickID(t, driver, italicBlockID)
	if got := state.session.Current().Text(); got != "# Title\n\nBodyX\n" || state.mode != renderedMode {
		t.Fatalf("italic toggle source=%q mode=%v", got, state.mode)
	}
}

func TestRenderedWholeParagraphLinkAppliesSafeURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# Title\r\n\r\nBody\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, linkBlockID)
	if !state.linkOpen || driver.Snapshot().Focused != linkInputID {
		t.Fatalf("link input not focused: open=%v focused=%v", state.linkOpen, driver.Snapshot().Focused)
	}
	if err := driver.Type("https://example.com/doc"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\r\n\r\n[Body](https://example.com/doc)\r\n\r\nTail\r\n" || state.mode != renderedMode || state.linkOpen {
		t.Fatalf("link action mode=%v open=%v source=%q", state.mode, state.linkOpen, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := "\xef\xbb\xbf# Title\r\n\r\n[Body](https://example.com/doc)\r\n\r\nTail\r\n"
	if err != nil || string(got) != want {
		t.Fatalf("saved link = %q, err=%v", got, err)
	}
}

func TestRenderedLinkRejectsUnsafeAddressAndCanCancel(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, linkBlockID)
	if err := driver.Type("javascript:alert(1)"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !state.linkOpen || state.session.Current().Text() != "Body\n" {
		t.Fatalf("unsafe link changed source=%q open=%v", state.session.Current().Text(), state.linkOpen)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.linkOpen || state.session.Current().Text() != "Body\n" {
		t.Fatal("canceling link changed document")
	}
}

func TestRenderedWholeBlockFormattingShortcuts(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if _, err := driver.ShortcutChord(primaryModifier(), 'b'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**Body**\n" {
		t.Fatalf("Cmd+B source=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'b'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'i'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "*Body*\n" {
		t.Fatalf("Cmd+I source=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'i'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'k'); err != nil {
		t.Fatal(err)
	}
	if !state.linkOpen || driver.Snapshot().Focused != linkInputID || state.session.Current().Text() != "Body\n" {
		t.Fatalf("Cmd+K open=%v source=%q", state.linkOpen, state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'b'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "Body\n" {
		t.Fatal("formatting shortcut mutated document while editing URL")
	}
}

func TestVirtualParagraphDoesNotAdvertiseUnavailableFormatting(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("Next"); err != nil {
		t.Fatal(err)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == boldBlockID || hit.ID == italicBlockID || hit.ID == linkBlockID {
			t.Fatalf("virtual input advertises inapplicable formatting: %+v", hit)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'b'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "Body\n\nNext\n" {
		t.Fatal("format shortcut unexpectedly changed virtual paragraph")
	}
}

func TestRenderedSelectedBoldKeepsMixedParagraphEditable(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(7, false)
	state.rich.input.MoveTo(12, true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, boldBlockID)
	if got := state.session.Current().Text(); got != "before **after**\n" || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("selected bold mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, boldBlockID)
	if got := state.session.Current().Text(); got != "before after\n" || state.mode != renderedMode {
		t.Fatalf("toggle selected bold mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedTypingThenBoldUndoKeepsDocumentHistory(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, boldBlockID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "BodyX\n" {
		t.Fatalf("undo format after typing = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "Body\n" {
		t.Fatalf("undo preceding typing = %q", got)
	}
}

func TestRenderedItalicSelectionBesideLinkKeepsURL(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("[link](https://example.com) tail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(5, false)
	state.rich.input.MoveTo(9, true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'i'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "[link](https://example.com) *tail*\n" || state.mode != renderedMode {
		t.Fatalf("italic selection mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedFormatCrossStyleSelectionLeavesSourceUntouched(t *testing.T) {
	state, driver := testDriver(t)
	source := "before **bold** after\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(5, false)
	state.rich.input.MoveTo(12, true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'b'); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || state.session.Current().Text() != source {
		t.Fatalf("cross-style formatting mode=%v source=%q", state.mode, state.session.Current().Text())
	}
}

func TestRenderedSelectedBoldUndoRedoKeepsRenderedView(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(7, false)
	state.rich.input.MoveTo(12, true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, boldBlockID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before after\n" || state.mode != renderedMode {
		t.Fatalf("selected bold undo mode=%v source=%q", state.mode, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before **after**\n" || state.mode != renderedMode {
		t.Fatalf("selected bold redo mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedLinkSelectionPreservesBoldAndDestination(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before **bold** after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(12, false)
	state.rich.input.MoveTo(17, true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, linkBlockID)
	if err := driver.Type("https://example.com/doc"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before **bold** [after](https://example.com/doc)\n" || state.mode != renderedMode {
		t.Fatalf("selected link mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedSelectedStrikethroughRemainsRendered(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(7, false)
	state.rich.input.MoveTo(12, true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, strikeBlockID)
	if got := state.session.Current().Text(); got != "before ~~after~~\n" || state.mode != renderedMode {
		t.Fatalf("strike selection mode=%v source=%q", state.mode, got)
	}
	if err := driver.Type("new"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before ~~new~~\n" {
		t.Fatalf("strike edit source=%q", got)
	}
}

func TestRenderedBlockMenuSwitchesQuoteAndListMarkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "block.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# Title\r\n\r\nBody\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	for _, step := range []struct {
		option int
		want   string
	}{{0, "> Body"}, {1, "- Body"}, {2, "1. Body"}} {
		clickID(t, driver, headingMenuID)
		clickID(t, driver, blockFormats.At(step.option))
		want := "# Title\r\n\r\n" + step.want + "\r\n\r\nTail\r\n"
		if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
			t.Fatalf("block option %d mode=%v source=%q message=%q", step.option, state.mode, got, state.message)
		}
	}
	clickID(t, driver, headingMenuID)
	clickID(t, driver, headingLevels.At(0))
	if got := state.session.Current().Text(); got != "# Title\r\n\r\nBody\r\n\r\nTail\r\n" || state.mode != renderedMode {
		t.Fatalf("return to paragraph mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, headingMenuID)
	clickID(t, driver, blockFormats.At(0))
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := "\xef\xbb\xbf# Title\r\n\r\n> Body\r\n\r\nTail\r\n"
	if err != nil || string(got) != want {
		t.Fatalf("saved block quote = %q, err=%v", got, err)
	}
}

func TestRenderedBlockMenuCreatesTaskAndTogglesCheckbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# Title\r\n\r\nBody\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, blockFormats.At(3))
	if got := state.session.Current().Text(); got != "# Title\r\n\r\n- [ ] Body\r\n\r\nTail\r\n" || state.mode != renderedMode {
		t.Fatalf("task action mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, taskChecks.At(1))
	if got := state.session.Current().Text(); got != "# Title\r\n\r\n- [x] Body\r\n\r\nTail\r\n" || state.mode != renderedMode {
		t.Fatalf("task check mode=%v source=%q", state.mode, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := "\xef\xbb\xbf# Title\r\n\r\n- [x] Body\r\n\r\nTail\r\n"
	if err != nil || string(got) != want {
		t.Fatalf("saved task = %q, err=%v", got, err)
	}
}

func TestRenderedCodeBlockCommandKeepsOtherSourceAndUndo(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\nBody\n\nTail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, blockFormats.At(4))
	if got := state.session.Current().Text(); got != "# Title\n\n```\nBody\n```\n\nTail\n" || state.mode != editMode {
		t.Fatalf("code block command mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, sourceID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("undo code block = %q", got)
	}
}

func TestRenderedTableCommandInsertsAfterParagraphWithoutRewritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	if err := os.WriteFile(path, []byte("# Title\r\n\r\nBody\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, blockFormats.At(5))
	want := "# Title\r\n\r\nBody\r\n\r\n| 列 1 | 列 2 |\r\n| --- | --- |\r\n|     |     |\r\n\r\nTail\r\n"
	if got := state.session.Current().Text(); got != want || state.mode != editMode {
		t.Fatalf("table command mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedWholeStrikethroughToggles(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, strikeBlockID)
	if got := state.session.Current().Text(); got != "~~Body~~\n" || state.mode != renderedMode {
		t.Fatalf("whole strike mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, strikeBlockID)
	if got := state.session.Current().Text(); got != "Body\n" || state.mode != renderedMode {
		t.Fatalf("whole strike toggle mode=%v source=%q", state.mode, got)
	}
}

// TestRenderedOrdinaryMarkdownSupportMatrix is the ordinary-Markdown support
// matrix for the rendered view. Each row records one ordinary syntax class and
// whether the rendered editor accepts it ("supported": a click focuses native
// input and typing patches only that block's source interval) or explicitly
// refuses it ("fallback": the click enters Markdown Source at the same block
// with a message and leaves every byte unchanged). Unsupported rows are the
// documented boundary: inline code spans, unknown HTML blocks, complex nested
// structures, and table cells with inline markup or escaped pipes.
func TestRenderedOrdinaryMarkdownSupportMatrix(t *testing.T) {
	type matrixAction int
	const (
		matrixRow matrixAction = iota
		matrixCell
	)
	type matrixCase struct {
		name      string
		source    string
		action    matrixAction
		cell      int
		supported bool
		want      string
	}
	cases := []matrixCase{
		{name: "paragraph", source: "Body\n", supported: true, want: "Body新\n"},
		{name: "quote", source: "> Quoted\n", supported: true, want: "> Quoted新\n"},
		{name: "unordered-list", source: "- Item\n", supported: true, want: "- Item新\n"},
		{name: "ordered-list", source: "1. Item\n", supported: true, want: "1. Item新\n"},
		{name: "task-list", source: "- [ ] Task\n", supported: true, want: "- [ ] Task新\n"},
		{name: "inline-bold", source: "before **bold** after\n", supported: true, want: "before **bold** after新\n"},
		{name: "inline-link", source: "前 [link](https://example.invalid) 后\n", supported: true, want: "前 [link](https://example.invalid) 后新\n"},
		{name: "table-cell", source: "| A | B |\n| --- | --- |\n| one | two |\n", action: matrixCell, cell: 3, supported: true, want: "| A | B |\n| --- | --- |\n| one | two新 |\n"},
		{name: "fenced-code", source: "```go\nbody\n```\n", supported: true, want: "```go\nbody新\n```\n"},
		{name: "inline-code-fallback", source: "前 `code` 后\n"},
		{name: "unknown-html-fallback", source: "<!--unknown-->\n"},
		{name: "nested-list-fallback", source: "- parent\n  - nested child\n"},
		{name: "nested-quote-list-fallback", source: "> - nested quote item\n"},
		{name: "marked-table-cell-fallback", source: "| **bold** | plain |\n| --- | --- |\n| a | b |\n", action: matrixCell, cell: 0},
		{name: "escaped-pipe-table-fallback", source: "| a \\| b | c |\n| --- | --- |\n| 1 | 2 |\n", action: matrixCell, cell: 0},
	}
	for level := 1; level <= 6; level++ {
		marker := strings.Repeat("#", level)
		cases = append(cases, matrixCase{
			name: marker + "-heading", source: marker + " Head\n", supported: true, want: marker + " Head新\n",
		})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "matrix.md")
			if err := os.WriteFile(path, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(1201, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			if test.action == matrixCell {
				clickID(t, driver, renderedTableCells.At(test.cell))
			} else {
				clickID(t, driver, renderedRows.At(0))
			}
			if !test.supported {
				if state.mode != editMode || driver.Snapshot().Focused != sourceID || state.session.Current().Text() != test.source || state.session.Current().Dirty() {
					t.Fatalf("%s fallback: mode=%v focus=%v source=%q dirty=%v", test.name, state.mode, driver.Snapshot().Focused, state.session.Current().Text(), state.session.Current().Dirty())
				}
				if !strings.Contains(state.message, "Markdown 源码模式") {
					t.Fatalf("%s fallback message=%q", test.name, state.message)
				}
				return
			}
			if state.mode != renderedMode || state.session.Current().Text() != test.source {
				t.Fatalf("%s activation: mode=%v source=%q", test.name, state.mode, state.session.Current().Text())
			}
			if err := driver.Type("新"); err != nil {
				t.Fatalf("%s typing: %v", test.name, err)
			}
			if got := state.session.Current().Text(); got != test.want || !state.session.Current().Dirty() {
				t.Fatalf("%s typed source=%q dirty=%v, want %q", test.name, got, state.session.Current().Dirty(), test.want)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != test.source || state.session.Current().Dirty() || state.mode != renderedMode {
				t.Fatalf("%s Undo=%q dirty=%v mode=%v", test.name, got, state.session.Current().Dirty(), state.mode)
			}
		})
	}
}

func TestRenderedModeRoundTripKeepsOneDirtyDocumentThenSavesAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.md")
	source := "# 标题\r\n\r\n正文\r\n\r\nTail\r\n"
	original := []byte("\ufeff" + source)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	documentID := state.session.Current().ID
	tabs := len(state.session.Tabs)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "# 标题\r\n\r\n正文新\r\n\r\nTail\r\n"
	for _, step := range []struct {
		id   gfx.ID
		mode viewMode
	}{
		{editModeID, editMode}, {renderedModeID, renderedMode}, {splitModeID, splitMode},
		{readModeID, readMode}, {renderedModeID, renderedMode}, {editModeID, editMode},
	} {
		clickID(t, driver, step.id)
		current := state.session.Current()
		if state.mode != step.mode || current.ID != documentID || current.Text() != want || !current.Dirty() || len(state.session.Tabs) != tabs {
			t.Fatalf("mode round trip mode=%v id=%d source=%q dirty=%v tabs=%d", state.mode, current.ID, current.Text(), current.Dirty(), len(state.session.Tabs))
		}
	}
	// One undo through the shared document history restores the whole edit.
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source || state.session.Current().Dirty() {
		t.Fatalf("round-trip Undo=%q dirty=%v", got, state.session.Current().Dirty())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("round-trip Redo=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != "\ufeff"+want {
		t.Fatalf("round-trip save=%q, %v", saved, err)
	}
	reopened := newModel()
	reopened.openFile(path)
	if got := reopened.session.Current().Text(); got != want || reopened.session.Current().Dirty() {
		t.Fatalf("reopened source=%q dirty=%v", got, reopened.session.Current().Dirty())
	}
	reopened.parsed.Parse(want)
	if len(reopened.parsed.Blocks) < 2 {
		t.Fatalf("reopened document parsed %d blocks", len(reopened.parsed.Blocks))
	}
	var visible strings.Builder
	for _, span := range reopened.parsed.Spans(&reopened.parsed.Blocks[1]) {
		visible.WriteString(span.Text)
	}
	if visible.String() != "正文新" {
		t.Fatalf("reopened rendered paragraph=%q", visible.String())
	}
}
