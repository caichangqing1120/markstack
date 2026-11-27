package main

import (
	"crypto/sha256"
	"testing"

	"github.com/vivaui/viva/text"
)

// TestRenderedNativeSampleRejectsImpossibleTopWithoutTouchingInput proves that
// a stale sample whose newest recorded edit cannot begin a native unit does not
// probe or replay the input. A probe would call BreakUndoGroup, so ordinary
// typing after the rejected sample would no longer coalesce with the earlier
// text.
func TestRenderedNativeSampleRejectsImpossibleTopWithoutTouchingInput(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		alter func(*renderedCrossEdit)
	}{
		{"nonvirtual", func(edit *renderedCrossEdit) { edit.virtualText = false }},
		{"off-position", func(edit *renderedCrossEdit) { edit.position++ }},
		{"fingerprint", func(edit *renderedCrossEdit) { edit.afterVisibleHash[0] ^= 0xff }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			input := text.NewEditor("")
			for _, character := range "abc" {
				input.Insert(string(character))
			}
			edit := renderedCrossEdit{
				virtualText:       true,
				position:          1,
				beforeVisibleHash: sha256.Sum256(nil),
				afterVisibleHash:  sha256.Sum256([]byte(input.String())),
			}
			scenario.alter(&edit)
			rich := renderedEditor{input: input, appliedEdits: 1}
			rich.crossUndo = []renderedCrossEdit{edit}
			canUndo, canRedo := input.CanUndo(), input.CanRedo()
			if rich.sampleNativeGroups() {
				t.Fatal("impossible top edit was published as a native group")
			}
			if input.String() != "abc" || input.CanUndo() != canUndo || input.CanRedo() != canRedo {
				t.Fatalf("rejected sample changed the input: value=%q undo=%v redo=%v", input.String(), input.CanUndo(), input.CanRedo())
			}
			for _, character := range "de" {
				input.Insert(string(character))
			}
			if !input.Undo() {
				t.Fatal("typing after the rejected sample had no undo")
			}
			if got := input.String(); got != "" {
				t.Fatalf("rejected sample broke typing coalescing: got %q, want the whole typed run undone", got)
			}
		})
	}
}
