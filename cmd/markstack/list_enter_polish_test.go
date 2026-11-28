package main

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/markdownedit"
)

func TestRenderedListEnterContinuesBeforeFollowingParagraphAndCode(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- first\n\nFollowing paragraph\n\n```go\nx := 1\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || !state.rich.virtual {
		t.Fatalf("unordered list Enter before paragraph fell out of rendered mode: mode=%v active=%v virtual=%v", state.mode, state.rich.active, state.rich.virtual)
	}
	if err := driver.Type("second"); err != nil {
		t.Fatal(err)
	}
	want := "- first\n- second\n\nFollowing paragraph\n\n```go\nx := 1\n```\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("unordered list middle Enter = %q, want %q (mode=%v)", got, want, state.mode)
	}
}

func TestRenderedOrderedListEnterIncrementsAcrossMultipleItemsBeforeTail(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("1. one\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("two"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("three"); err != nil {
		t.Fatal(err)
	}
	want := "1. one\n2. two\n3. three\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("ordered list multi-Enter = %q, want %q (mode=%v)", got, want, state.mode)
	}
}

func TestRenderedListTextInsertNewlineContinuesItems(t *testing.T) {
	cases := []struct {
		name   string
		source string
		typed  string
		want   string
	}{
		{"unordered", "- alpha\n\nEnd\n", "beta", "- alpha\n- beta\n\nEnd\n"},
		{"ordered", "1. alpha\n\nEnd\n", "beta", "1. alpha\n2. beta\n\nEnd\n"},
		{"task", "- [ ] alpha\n\nEnd\n", "beta", "- [ ] alpha\n- [ ] beta\n\nEnd\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace(tc.source)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline}); err != nil {
				t.Fatal(err)
			}
			if state.mode != renderedMode || !state.rich.active || !state.rich.virtual {
				t.Fatalf("TextInsertNewline did not open next virtual list item: mode=%v active=%v virtual=%v", state.mode, state.rich.active, state.rich.virtual)
			}
			if err := driver.Type(tc.typed); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != tc.want || state.mode != renderedMode {
				t.Fatalf("TextInsertNewline result = %q, want %q (mode=%v)", got, tc.want, state.mode)
			}
		})
	}
}

func TestRenderedListEnterInMiddleSplitsItemAndIncrementsNumber(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("1. hello world\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(5, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	want := "1. hello\n2. world\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("mid-item split = %q, want %q (mode=%v active=%v)", got, want, state.mode, state.rich.active)
	}
}

func TestRenderedEmptyToolbarListEnterExitsCleanly(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, formatOrderedListID)
	if got := state.session.Current().Text(); got != "1. " || state.mode != renderedMode {
		t.Fatalf("toolbar ordered list = %q (mode=%v)", got, state.mode)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "" || state.mode != renderedMode || state.rich.block.Kind != markdownedit.Paragraph {
		t.Fatalf("empty toolbar ordered list Enter = %q (mode=%v kind=%q)", got, state.mode, state.rich.block.Kind)
	}
}
