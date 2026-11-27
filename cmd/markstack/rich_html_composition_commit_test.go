package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

func TestRichHTMLSelectionDoesNotCommitPreedit(test *testing.T) {
	for _, event := range []app.TextEvent{
		{Kind: app.TextSetSelection, Selection: app.TextRange{Start: 2, End: 2}},
		{Kind: app.TextCommandEvent, Command: app.TextMoveLeft},
		{Kind: app.TextCommandEvent, Command: app.TextSelectAll},
	} {
		filename := filepath.Join(test.TempDir(), "selection.mh")
		const original = "<p>abc</p>"
		if err := os.WriteFile(filename, []byte(original), 0640); err != nil {
			test.Fatal(err)
		}
		state, driver := testDriver(test)
		if !state.openFile(filename) {
			test.Fatal(state.message)
		}
		if err := driver.Resize(1200, 780); err != nil {
			test.Fatal(err)
		}
		clickID(test, driver, htmlLeafRows.At(0))
		input := state.html.input
		input.SetSelection(text.Range{Start: 1, End: 2})
		apply := func(event app.TextEvent) {
			test.Helper()
			if _, err := state.onHTMLTextEvent(event); err != nil {
				test.Fatal(err)
			}
		}
		apply(app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}})
		apply(event)
		state.saveActive(false)
		saved, err := os.ReadFile(filename)
		current := state.session.Current()
		if err != nil || string(saved) != original || current.Text() != original || current.Dirty() || len(state.session.RecoverySnapshot().Drafts) != 0 {
			test.Fatalf("selection event %+v promoted preedit: source=%q disk=%q err=%v", event, current.Text(), saved, err)
		}
		if _, composing := input.Composition(); !composing || input.CommittedString() != "abc" {
			test.Fatal("selection-only event ended the active preedit")
		}
		apply(app.TextEvent{Kind: app.TextInsert, Text: "X"})
		if current.Text() != "<p>aXc</p>" {
			test.Fatalf("explicit commit after selection event = %q", current.Text())
		}
	}
}

func TestRichHTMLSameTextCompositionCommit(test *testing.T) {
	for _, encoding := range []string{"lf", "bom-crlf"} {
		for _, marked := range []string{"x", "\u4f60", "", "b"} {
			endings := []string{"insert", "explicit-insert", "unmark", "cancel"}
			if marked != "b" {
				endings = append(endings, "different-insert")
			}
			for _, ending := range endings {
				for _, backward := range []bool{false, true} {
					name := encoding + "/" + ending + "/" + marked + "/forward"
					if backward {
						name = encoding + "/" + ending + "/" + marked + "/backward"
					}
					test.Run(name, func(context *testing.T) {
						directory := context.TempDir()
						for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
							context.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
						}
						original := "<!doctype html>\n<!-- \u539f\u6587 -->\n<p data-keep='yes'>abc</p>\n<custom untouched='&amp;'>opaque &#97;</custom>\n<p>abc</p>\n"
						if encoding == "bom-crlf" {
							original = strings.ReplaceAll(original, "\n", "\r\n")
						}
						encode := func(value string) []byte {
							if encoding == "bom-crlf" {
								return []byte("\ufeff" + value)
							}
							return []byte(value)
						}
						filename := filepath.Join(directory, "sample.mh")
						if err := os.WriteFile(filename, encode(original), 0640); err != nil {
							context.Fatal(err)
						}
						state, driver := testDriver(context)
						if !state.openFile(filename) {
							context.Fatal(state.message)
						}
						if err := driver.Resize(1200, 780); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, htmlLeafRows.At(0))
						current := state.session.Current()
						input := state.html.input
						if !state.html.active || input == nil || input.String() != "abc" {
							context.Fatal("supported HTML leaf did not activate")
						}
						apply := func(event app.TextEvent) {
							context.Helper()
							if _, err := state.onHTMLTextEvent(event); err != nil {
								context.Fatal(err)
							}
						}
						assertState := func(stage, source, visible string) {
							context.Helper()
							if current.Text() != source || state.html.input.String() != visible || state.html.input.CommittedString() != visible {
								context.Fatalf("%s: source=%q visible=%q committed=%q, want %q / %q", stage, current.Text(), state.html.input.String(), state.html.input.CommittedString(), source, visible)
							}
							if _, composing := state.html.input.Composition(); composing {
								context.Fatalf("%s: composition did not end", stage)
							}
							if err := state.session.Save(state.session.Active, ""); err != nil {
								context.Fatal(err)
							}
							data, err := os.ReadFile(filename)
							info, statErr := os.Stat(filename)
							if err != nil || statErr != nil || !bytes.Equal(data, encode(source)) || info.Mode().Perm() != 0640 {
								context.Fatalf("%s: saved bytes=%q read=%v stat=%v", stage, data, err, statErr)
							}
							if current.Dirty() || len(state.session.RecoverySnapshot().Drafts) != 0 {
								context.Fatalf("%s: save left a dirty/recovery draft", stage)
							}
						}
						target := text.Range{Start: 1, End: 2}
						if backward {
							target.Start, target.End = target.End, target.Start
						}
						input.SetSelection(target)
						apply(app.TextEvent{Kind: app.TextSetMarked, Text: marked, MarkedSelection: app.TextRange{Start: utf8.RuneCountInString(marked), End: utf8.RuneCountInString(marked)}})
						composition, composing := input.Composition()
						if !composing || current.Text() != original || current.Dirty() || input.CommittedString() != "abc" || len(state.session.RecoverySnapshot().Drafts) != 0 {
							context.Fatal("preedit changed committed source or recovery")
						}
						state.saveActive(false)
						if state.message != session.ErrCompositionPending.Error() {
							context.Fatalf("Save did not refuse preedit: %q", state.message)
						}
						if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
							context.Fatalf("pending composition reached disk: %q / %v", data, err)
						}
						commit := app.TextEvent{Kind: app.TextInsert, Text: marked}
						replacement := marked
						switch ending {
						case "explicit-insert":
							commit.HasReplacement = true
							commit.Replacement = app.TextRange{Start: composition.Start, End: composition.End}
						case "unmark":
							commit.Kind = app.TextUnmark
						case "different-insert":
							commit.Text, replacement = "X", "X"
						case "cancel":
							commit.Kind, replacement = app.TextCancelComposition, "b"
						}
						apply(commit)
						visible := "a" + replacement + "c"
						wanted := strings.Replace(original, ">abc</p>", ">a"+replacement+"c</p>", 1)
						if current.Text() != wanted || current.Dirty() != (wanted != original) {
							context.Fatalf("commit lost source/dirty transition: %q dirty=%v, want %q", current.Text(), current.Dirty(), wanted)
						}
						if wanted != original {
							drafts := state.session.RecoverySnapshot().Drafts
							if len(drafts) != 1 || drafts[0].Body != wanted {
								context.Fatalf("committed recovery does not match source: %+v", drafts)
							}
						}
						assertState("commit", wanted, visible)
						apply(app.TextEvent{Kind: app.TextUnmark})
						if wanted == original {
							if current.Editor.CanUndo() {
								context.Fatal("cancel/no-op commit invented source history")
							}
							return
						}
						for round := 0; round < 2; round++ {
							apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
							assertState("undo", original, "abc")
							if state.html.input.Selection() != (text.Range{Start: 1, End: 2}) || state.html.input.Caret() != target.End {
								context.Fatalf("Undo lost original selection direction: %+v caret=%d", state.html.input.Selection(), state.html.input.Caret())
							}
							apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextRedo})
							assertState("redo", wanted, visible)
						}
						apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
						assertState("final", original, "abc")
						if current.Editor.CanUndo() {
							context.Fatal("composition created more than one source undo unit")
						}
					})
				}
			}
		}
	}
}

func TestRichHTMLSameTextCommitInterleavedHistory(test *testing.T) {
	for _, encoding := range []string{"lf", "bom-crlf"} {
		for _, ending := range []string{"insert", "explicit-insert", "unmark"} {
			for _, backward := range []bool{false, true} {
				name := encoding + "/" + ending + "/forward"
				if backward {
					name = encoding + "/" + ending + "/backward"
				}
				test.Run(name, func(context *testing.T) {
					directory := context.TempDir()
					for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
						context.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
					}
					original := "<!-- \u4fdd\u7559 -->\n<p data-x='1'>abc</p>\n<custom>&amp; untouched</custom>\n<p>abc</p>\n"
					if encoding == "bom-crlf" {
						original = strings.ReplaceAll(original, "\n", "\r\n")
					}
					encode := func(source string) []byte {
						if encoding == "bom-crlf" {
							return []byte("\ufeff" + source)
						}
						return []byte(source)
					}
					filename := filepath.Join(directory, "history.mh")
					if err := os.WriteFile(filename, encode(original), 0640); err != nil {
						context.Fatal(err)
					}
					state, driver := testDriver(context)
					if !state.openFile(filename) {
						context.Fatal(state.message)
					}
					if err := driver.Resize(1200, 780); err != nil {
						context.Fatal(err)
					}
					clickID(context, driver, htmlLeafRows.At(0))
					current := state.session.Current()
					apply := func(event app.TextEvent) {
						context.Helper()
						if _, err := state.onHTMLTextEvent(event); err != nil {
							context.Fatal(err)
						}
					}
					assertSaved := func(visible string) {
						context.Helper()
						wanted := strings.Replace(original, ">abc</p>", ">"+visible+"</p>", 1)
						if current.Text() != wanted || state.html.input.String() != visible {
							context.Fatalf("history source=%q visible=%q want %q / %q", current.Text(), state.html.input.String(), wanted, visible)
						}
						if err := state.session.Save(state.session.Active, ""); err != nil {
							context.Fatal(err)
						}
						data, err := os.ReadFile(filename)
						info, statErr := os.Stat(filename)
						if err != nil || statErr != nil || !bytes.Equal(data, encode(wanted)) || info.Mode().Perm() != 0640 {
							context.Fatalf("history save bytes=%q read=%v stat=%v", data, err, statErr)
						}
					}
					state.html.input.SetSelection(text.Range{Start: 0, End: 1})
					apply(app.TextEvent{Kind: app.TextPaste, Text: "A"})
					target := text.Range{Start: 1, End: 2}
					if backward {
						target.Start, target.End = target.End, target.Start
					}
					state.html.input.SetSelection(target)
					apply(app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}})
					marked, composing := state.html.input.Composition()
					if !composing {
						context.Fatal("missing composition")
					}
					apply(app.TextEvent{Kind: app.TextSetMarked, Text: "nihao", MarkedSelection: app.TextRange{Start: 5, End: 5}, HasReplacement: true, Replacement: app.TextRange{Start: marked.Start, End: marked.End}})
					prior := strings.Replace(original, ">abc</p>", ">Abc</p>", 1)
					drafts := state.session.RecoverySnapshot().Drafts
					if current.Text() != prior || len(drafts) != 1 || drafts[0].Body != prior {
						context.Fatal("continued preedit entered source or dirty recovery")
					}
					marked, _ = state.html.input.Composition()
					commit := app.TextEvent{Kind: app.TextInsert, Text: "nihao"}
					if ending == "explicit-insert" {
						commit.HasReplacement = true
						commit.Replacement = app.TextRange{Start: marked.Start, End: marked.End}
					} else if ending == "unmark" {
						commit.Kind = app.TextUnmark
					}
					apply(commit)
					assertSaved("Anihaoc")
					apply(app.TextEvent{Kind: app.TextPaste, Text: "Y"})
					assertSaved("AnihaoYc")
					for round := 0; round < 2; round++ {
						for _, visible := range []string{"Anihaoc", "Abc", "abc"} {
							apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
							assertSaved(visible)
							if visible == "Abc" && (state.html.input.Selection() != (text.Range{Start: 1, End: 2}) || state.html.input.Caret() != target.End) {
								context.Fatal("interleaved Undo lost the composition's original selection")
							}
						}
						if current.Editor.CanUndo() {
							context.Fatal("composition introduced an extra Undo unit")
						}
						for _, visible := range []string{"Abc", "Anihaoc", "AnihaoYc"} {
							apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextRedo})
							assertSaved(visible)
						}
					}
					for range 3 {
						apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
					}
					assertSaved("abc")
				})
			}
		}
	}
}

func TestRichHTMLSameTextCommitPreservesUntouchedEntities(t *testing.T) {
	const original = "<!-- keep -->\r\n<p data-keep='&#x42;'>甲 &#x26; b &#60; &#x597D; &NotEqualTilde; 尾</p>\r\n<custom>opaque &#97;</custom>\r\n"
	const marked = "你<&"
	committed := strings.Replace(original, " b ", " 你&lt;&amp; ", 1)
	for _, ending := range []string{"insert", "explicit-reverse", "unmark", "cancel"} {
		t.Run(ending, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "entities.mh")
			if err := os.WriteFile(filename, []byte("\ufeff"+original), 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filename, 0640); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			if !state.openFile(filename) {
				t.Fatal(state.message)
			}
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, htmlLeafRows.At(0))
			input := state.html.input
			if input == nil || input.String() != "甲 & b < 好 ≂̸ 尾" {
				t.Fatal("entity-bearing leaf did not activate")
			}
			selection := text.Range{Start: 4, End: 5}
			if ending == "explicit-reverse" {
				selection.Start, selection.End = selection.End, selection.Start
			}
			input.SetSelection(selection)
			apply := func(event app.TextEvent) {
				t.Helper()
				if _, err := state.onHTMLTextEvent(event); err != nil {
					t.Fatal(err)
				}
			}
			assertDisk := func(expected string) {
				t.Helper()
				saved, err := os.ReadFile(filename)
				if err != nil || string(saved) != "\ufeff"+expected {
					t.Fatalf("saved source = %q, %v; want %q", saved, err, expected)
				}
				info, err := os.Stat(filename)
				if err != nil || info.Mode().Perm() != 0640 {
					t.Fatalf("mode changed: %v, %v", info, err)
				}
			}
			current := state.session.Current()
			save := func(expected string) {
				t.Helper()
				state.saveActive(false)
				assertDisk(expected)
				if current.Dirty() || len(state.session.RecoverySnapshot().Drafts) != 0 {
					t.Fatal("Save left a dirty draft")
				}
			}
			apply(app.TextEvent{Kind: app.TextSetMarked, Text: marked, MarkedSelection: app.TextRange{Start: 3, End: 3}})
			composition, active := input.Composition()
			state.saveActive(false)
			if !active || state.message != session.ErrCompositionPending.Error() || current.Text() != original || len(state.session.RecoverySnapshot().Drafts) != 0 {
				t.Fatal("preedit changed the document or its recovery state")
			}
			assertDisk(original)
			event := app.TextEvent{Kind: app.TextInsert, Text: marked}
			switch ending {
			case "explicit-reverse":
				event.HasReplacement = true
				event.Replacement = app.TextRange{Start: composition.Start, End: composition.End}
			case "unmark":
				event.Kind = app.TextUnmark
			case "cancel":
				event.Kind = app.TextCancelComposition
			}
			apply(event)
			if ending == "cancel" {
				if current.Text() != original || current.Editor.CanUndo() || len(state.session.RecoverySnapshot().Drafts) != 0 {
					t.Fatal("cancel changed source, history or recovery")
				}
				save(original)
				return
			}
			drafts := state.session.RecoverySnapshot().Drafts
			if current.Text() != committed || len(drafts) != 1 || drafts[0].Body != committed {
				t.Errorf("commit rewrote untouched source: %q; want %q", current.Text(), committed)
			}
			save(committed)
			apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
			save(original)
			if current.Editor.CanUndo() || input.Selection() != (text.Range{Start: 4, End: 5}) || input.Caret() != selection.End {
				t.Fatal("Undo did not restore the oriented selection in one step")
			}
			apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextRedo})
			save(committed)
		})
	}
}

func TestRichHTMLRejectedCompositionCommitKeepsCommittedInputSelectionHistory(test *testing.T) {
	const rejected = "keep\nOUT"
	for _, encoding := range []string{"lf", "bom-crlf"} {
		for _, ending := range []string{"insert", "explicit-insert", "unmark"} {
			for _, backward := range []bool{false, true} {
				name := encoding + "/" + ending + "/forward"
				if backward {
					name = encoding + "/" + ending + "/backward"
				}
				test.Run(name, func(context *testing.T) {
					directory := context.TempDir()
					private := map[string]string{}
					for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
						path := filepath.Join(directory, strings.ToLower(key)+".json")
						private[key] = path
						context.Setenv("MARKSTACK_"+key+"_PATH", path)
					}
					original := "<!-- \u4fdd\u7559 -->\n<p data-keep='yes'>abc</p>\n<custom untouched='&amp;'>opaque &#97;</custom>\n"
					if encoding == "bom-crlf" {
						original = strings.ReplaceAll(original, "\n", "\r\n")
					}
					encode := func(value string) []byte {
						if encoding == "bom-crlf" {
							return []byte("\ufeff" + value)
						}
						return []byte(value)
					}
					filename := filepath.Join(directory, "rejected.mh")
					if err := os.WriteFile(filename, encode(original), 0640); err != nil {
						context.Fatal(err)
					}
					if err := os.Chmod(filename, 0640); err != nil {
						context.Fatal(err)
					}
					state, driver := testDriver(context)
					if !state.openFile(filename) {
						context.Fatal(state.message)
					}
					if err := driver.Resize(1200, 780); err != nil {
						context.Fatal(err)
					}
					clickID(context, driver, htmlLeafRows.At(0))
					current := state.session.Current()
					input := state.html.input
					if !state.html.active || input == nil || input.String() != "abc" {
						context.Fatal("supported HTML leaf did not activate")
					}
					apply := func(event app.TextEvent) {
						context.Helper()
						if _, err := state.onHTMLTextEvent(event); err != nil {
							context.Fatal(err)
						}
					}
					// Every private state file must stay free of the refused preedit.
					saved := func(stage, expected string) {
						context.Helper()
						if err := state.session.Save(state.session.Active, ""); err != nil {
							context.Fatal(err)
						}
						data, err := os.ReadFile(filename)
						info, statErr := os.Stat(filename)
						if err != nil || statErr != nil || !bytes.Equal(data, encode(expected)) || info.Mode().Perm() != 0640 {
							context.Fatalf("%s: saved bytes=%q read=%v stat=%v, want %q", stage, data, err, statErr, expected)
						}
						if current.Dirty() || len(state.session.RecoverySnapshot().Drafts) != 0 {
							context.Fatalf("%s: save left a dirty/recovery draft", stage)
						}
						for key, path := range private {
							if data, err := os.ReadFile(path); err == nil && bytes.Contains(data, []byte("OUT")) {
								context.Fatalf("%s: %s state leaked the refused text: %q", stage, key, data)
							}
						}
					}
					// A valid edit first, so its own Undo selection is distinguishable
					// from any caret a rejected commit could leave behind.
					input.SetSelection(text.Range{Start: 0, End: 1})
					apply(app.TextEvent{Kind: app.TextPaste, Text: "Z"})
					valid := strings.Replace(original, ">abc</p>", ">Zbc</p>", 1)
					if current.Text() != valid || !current.Dirty() || input.String() != "Zbc" {
						context.Fatalf("valid edit = %q / %q", current.Text(), input.String())
					}
					saved("valid", valid)
					target := text.Range{Start: 1, End: 2}
					if backward {
						target.Start, target.End = target.End, target.Start
					}
					input.SetSelection(target)
					apply(app.TextEvent{Kind: app.TextSetMarked, Text: rejected, MarkedSelection: app.TextRange{Start: utf8.RuneCountInString(rejected), End: utf8.RuneCountInString(rejected)}})
					composition, composing := input.Composition()
					if !composing || current.Text() != valid || input.CommittedString() != "Zbc" {
						context.Fatal("preedit changed committed source")
					}
					commit := app.TextEvent{Kind: app.TextInsert, Text: rejected}
					switch ending {
					case "explicit-insert":
						commit.HasReplacement = true
						commit.Replacement = app.TextRange{Start: composition.Start, End: composition.End}
					case "unmark":
						commit.Kind = app.TextUnmark
					}
					apply(commit)
					if current.Text() != valid || current.Dirty() || state.html.input.CommittedString() != "Zbc" || state.html.input.String() != "Zbc" || state.message == "" {
						context.Fatalf("rejected commit left uncommitted input: source=%q native=%q message=%q", current.Text(), state.html.input.String(), state.message)
					}
					if _, composing := state.html.input.Composition(); composing {
						context.Fatal("rejected commit left the composition open")
					}
					if state.html.input != input {
						context.Fatal("rejected commit replaced the leaf's input editor")
					}
					if state.html.input.Selection() != (text.Range{Start: 1, End: 2}) || state.html.input.Caret() != target.End {
						context.Fatalf("rejected commit lost the oriented selection: %+v caret=%d", state.html.input.Selection(), state.html.input.Caret())
					}
					if !state.html.input.CanUndo() {
						context.Fatal("rejected commit discarded the leaf's history")
					}
					if drafts := state.session.RecoverySnapshot().Drafts; len(drafts) != 0 {
						context.Fatalf("rejected commit drafted the refused text: %+v", drafts)
					}
					saved("rejected", valid)
					apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
					if current.Text() != original || input.String() != "abc" {
						context.Fatalf("Undo after rejection = %q / %q", current.Text(), input.String())
					}
					if input.Selection() != (text.Range{Start: 0, End: 1}) || input.Caret() != 1 {
						context.Fatalf("Undo after rejection lost the prior edit's selection: %+v caret=%d", input.Selection(), input.Caret())
					}
					if drafts := state.session.RecoverySnapshot().Drafts; len(drafts) != 1 || drafts[0].Body != original {
						context.Fatalf("Undo after rejection drafted %+v", drafts)
					}
					saved("undo", original)
					apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextRedo})
					if current.Text() != valid || input.String() != "Zbc" {
						context.Fatalf("Redo after rejection = %q / %q", current.Text(), input.String())
					}
					if input.Selection() != (text.Range{Start: 1, End: 1}) || input.Caret() != 1 {
						context.Fatalf("Redo after rejection lost the prior edit's caret: %+v caret=%d", input.Selection(), input.Caret())
					}
					if drafts := state.session.RecoverySnapshot().Drafts; len(drafts) != 1 || drafts[0].Body != valid {
						context.Fatalf("Redo after rejection drafted %+v", drafts)
					}
					saved("redo", valid)
					apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
					if current.Text() != original || input.String() != "abc" || input.CanUndo() {
						context.Fatalf("rejected commit left an extra Undo unit: %q / %q undo=%v", current.Text(), input.String(), input.CanUndo())
					}
					saved("final", original)
				})
			}
		}
	}
}

func TestRichHTMLCompositionCommitReplacingInvalidMarkKeepsOneStepHistory(test *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "implicit"
		if explicit {
			name = "explicit-insert"
		}
		test.Run(name, func(context *testing.T) {
			directory := context.TempDir()
			for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
				context.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
			}
			original := "<p data-keep='yes'>abc</p>\n<custom untouched='&amp;'>opaque &#97;</custom>\n"
			filename := filepath.Join(directory, "replaced.mh")
			if err := os.WriteFile(filename, []byte(original), 0640); err != nil {
				context.Fatal(err)
			}
			if err := os.Chmod(filename, 0640); err != nil {
				context.Fatal(err)
			}
			state, driver := testDriver(context)
			if !state.openFile(filename) {
				context.Fatal(state.message)
			}
			if err := driver.Resize(1200, 780); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, htmlLeafRows.At(0))
			current := state.session.Current()
			input := state.html.input
			apply := func(event app.TextEvent) {
				context.Helper()
				if _, err := state.onHTMLTextEvent(event); err != nil {
					context.Fatal(err)
				}
			}
			input.SetSelection(text.Range{Start: 0, End: 1})
			apply(app.TextEvent{Kind: app.TextPaste, Text: "Z"})
			valid := strings.Replace(original, ">abc</p>", ">Zbc</p>", 1)
			if current.Text() != valid {
				context.Fatalf("valid edit = %q", current.Text())
			}
			target := text.Range{Start: 1, End: 2}
			input.SetSelection(target)
			apply(app.TextEvent{Kind: app.TextSetMarked, Text: "keep\nOUT", MarkedSelection: app.TextRange{Start: 8, End: 8}})
			composition, composing := input.Composition()
			if !composing {
				context.Fatal("missing composition")
			}
			commit := app.TextEvent{Kind: app.TextInsert, Text: "ok"}
			if explicit {
				commit.HasReplacement = true
				commit.Replacement = app.TextRange{Start: composition.Start, End: composition.End}
			}
			apply(commit)
			committed := strings.Replace(original, ">abc</p>", ">Zokc</p>", 1)
			if current.Text() != committed || input.String() != "Zokc" || input.CommittedString() != "Zokc" || state.message != "" {
				context.Fatalf("commit = %q / %q message=%q, want %q", current.Text(), input.String(), state.message, committed)
			}
			if _, composing := input.Composition(); composing {
				context.Fatal("composition stayed open")
			}
			if err := state.session.Save(state.session.Active, ""); err != nil {
				context.Fatal(err)
			}
			data, err := os.ReadFile(filename)
			info, statErr := os.Stat(filename)
			if err != nil || statErr != nil || string(data) != committed || info.Mode().Perm() != 0640 || current.Dirty() || len(state.session.RecoverySnapshot().Drafts) != 0 {
				context.Fatalf("saved=%q read=%v stat=%v dirty=%v", data, err, statErr, current.Dirty())
			}
			for step, expected := range []string{valid, original} {
				apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
				if current.Text() != expected {
					context.Fatalf("Undo %d = %q, want %q", step+1, current.Text(), expected)
				}
				if step == 0 && (input.String() != "Zbc" || input.Selection() != target || input.Caret() != target.End) {
					context.Fatalf("Undo 1 leaf = %q selection=%+v caret=%d", input.String(), input.Selection(), input.Caret())
				}
				if step == 1 && (input.String() != "abc" || input.CanUndo()) {
					context.Fatalf("Undo 2 leaf = %q undo=%v", input.String(), input.CanUndo())
				}
			}
			for _, expected := range []string{valid, committed} {
				apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextRedo})
				if current.Text() != expected {
					context.Fatalf("Redo = %q, want %q", current.Text(), expected)
				}
			}
		})
	}
}

func TestRichHTMLCompositionCommitOutsideMarkKeepsInvalidText(test *testing.T) {
	directory := test.TempDir()
	for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
		test.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
	}
	original := "<p data-keep='yes'>abc</p>\n<custom untouched='&amp;'>opaque &#97;</custom>\n"
	filename := filepath.Join(directory, "outside.mh")
	if err := os.WriteFile(filename, []byte(original), 0640); err != nil {
		test.Fatal(err)
	}
	if err := os.Chmod(filename, 0640); err != nil {
		test.Fatal(err)
	}
	state, driver := testDriver(test)
	if !state.openFile(filename) {
		test.Fatal(state.message)
	}
	if err := driver.Resize(1200, 780); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, htmlLeafRows.At(0))
	current := state.session.Current()
	input := state.html.input
	apply := func(event app.TextEvent) {
		test.Helper()
		if _, err := state.onHTMLTextEvent(event); err != nil {
			test.Fatal(err)
		}
	}
	input.SetSelection(text.Range{Start: 0, End: 1})
	apply(app.TextEvent{Kind: app.TextPaste, Text: "Z"})
	valid := strings.Replace(original, ">abc</p>", ">Zbc</p>", 1)
	if err := state.session.Save(state.session.Active, ""); err != nil {
		test.Fatal(err)
	}
	target := text.Range{Start: 1, End: 2}
	input.SetSelection(target)
	apply(app.TextEvent{Kind: app.TextSetMarked, Text: "keep\nOUT", MarkedSelection: app.TextRange{Start: 8, End: 8}})
	apply(app.TextEvent{Kind: app.TextInsert, Text: "Q", HasReplacement: true, Replacement: app.TextRange{Start: 0, End: 1}})
	if current.Text() != valid || current.Dirty() || input.String() != "Zbc" || input.CommittedString() != "Zbc" || state.message == "" {
		test.Fatalf("mark outside the replacement reached source: %q / %q message=%q", current.Text(), input.String(), state.message)
	}
	if _, composing := input.Composition(); composing {
		test.Fatal("refused commit left the composition open")
	}
	if input.Selection() != target || input.Caret() != target.End || !input.CanUndo() {
		test.Fatalf("refused commit lost selection or history: %+v caret=%d undo=%v", input.Selection(), input.Caret(), input.CanUndo())
	}
	if drafts := state.session.RecoverySnapshot().Drafts; len(drafts) != 0 {
		test.Fatalf("refused commit drafted %+v", drafts)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		test.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	info, statErr := os.Stat(filename)
	if err != nil || statErr != nil || string(data) != valid || info.Mode().Perm() != 0640 {
		test.Fatalf("saved=%q read=%v stat=%v", data, err, statErr)
	}
	apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
	if current.Text() != original || input.String() != "abc" || input.CanUndo() {
		test.Fatalf("Undo after refusal = %q / %q undo=%v", current.Text(), input.String(), input.CanUndo())
	}
}
