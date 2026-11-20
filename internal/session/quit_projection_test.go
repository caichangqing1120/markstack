package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"markstack/internal/workspace"
)

func TestQuitWithPendingProjectionKeepsSaveAndDecisionContract(test *testing.T) {
	for _, fixture := range crossKindFixtures() {
		for _, variant := range crossKindVariants() {
			for _, ending := range []string{"save", "discard", "cancel", "save-as", "external-conflict"} {
				test.Run(fixture.name+"/"+variant.name+"/"+ending, func(context *testing.T) {
					state, document, index, filename, original := openCrossKind(context, context.TempDir(), fixture, variant)
					pending := true
					predicate := func(candidate *Document) bool { return candidate == document && pending }
					if state.RequestQuitWithPending(predicate) || state.Pending() == nil || state.Pending().Document != document || document.Dirty() || document.Composing() {
						context.Fatal("clean projected state did not enter a Quit confirmation")
					}
					if state.RequestQuit() || state.Pending() == nil || state.Pending().Document != document {
						context.Fatal("repeated Quit replaced the active transition")
					}
					if err := state.Save(index, ""); !errors.Is(err, ErrCompositionPending) {
						context.Fatalf("direct Save ignored projected composition: %v", err)
					}
					if completed, err := state.Resolve(Save, ""); completed || !errors.Is(err, ErrCompositionPending) || state.Pending() == nil {
						context.Fatalf("confirmation Save lost pending state: completed=%v err=%v", completed, err)
					}
					requireBytes(context, filename, original, "refused projected Save")
					pending = false
					if state.Pending() == nil || state.Pending().Document != document {
						context.Fatal("ending composition erased confirmation ownership")
					}
					decision := Save
					if ending == "discard" {
						decision = Discard
					} else if ending == "cancel" {
						decision = Cancel
					} else if ending == "save-as" {
						target := filepath.Join(filepath.Dir(filename), "existing"+filepath.Ext(filename))
						if err := state.Save(index, target); err != nil {
							context.Fatal(err)
						}
						if state.Pending() == nil {
							context.Fatal("Save As changed pending document identity")
						}
					}
					wantedDisk := original
					if ending == "external-conflict" {
						wantedDisk = withVariant(variant, "external\n")
						if err := os.WriteFile(filename, wantedDisk, 0640); err != nil {
							context.Fatal(err)
						}
						if completed, err := state.Resolve(Save, ""); completed || !errors.Is(err, workspace.ErrConflict) || state.Pending() == nil || state.Pending().Document != document {
							context.Fatalf("external conflict lost pending Quit: completed=%v err=%v", completed, err)
						}
						decision = Cancel
					}
					completed, err := state.Resolve(decision, "")
					if err != nil || completed != (decision != Cancel) || state.Pending() != nil || state.pending != nil {
						context.Fatalf("decision did not finish coherently: completed=%v err=%v pending=%v", completed, err, state.Pending())
					}
					if !state.RequestQuit() {
						context.Fatal("completed transition retained the projected predicate")
					}
					requireBytes(context, filename, wantedDisk, "final protected disk")
				})
			}
		}
	}
}

func TestQuitWithPendingIgnoresForeignIdentityAndNilPredicate(test *testing.T) {
	for _, scenario := range []string{"nil", "false", "foreign-same-id"} {
		test.Run(scenario, func(context *testing.T) {
			state := New()
			foreign := *state.Current()
			var predicate func(*Document) bool
			if scenario == "false" {
				predicate = func(*Document) bool { return false }
			} else if scenario == "foreign-same-id" {
				predicate = func(document *Document) bool { return document == &foreign }
			}
			if !state.RequestQuitWithPending(predicate) || state.Pending() != nil || state.pending != nil {
				context.Fatal("nil/false/non-tab projection created a phantom confirmation")
			}
		})
	}
}
