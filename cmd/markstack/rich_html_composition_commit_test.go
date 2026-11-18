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

func TestRichHTMLRejectedCompositionCommitRestoresCommittedInput(test *testing.T) {
	for _, ending := range []app.TextEventKind{app.TextInsert, app.TextUnmark} {
		test.Run(map[app.TextEventKind]string{app.TextInsert: "insert", app.TextUnmark: "unmark"}[ending], func(context *testing.T) {
			directory := context.TempDir()
			for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
				context.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
			}
			filename := filepath.Join(directory, "rejected.mh")
			original := "<p>abc</p>\n"
			if err := os.WriteFile(filename, []byte(original), 0640); err != nil {
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
			state.html.input.SetSelection(text.Range{Start: 1, End: 2})
			for _, event := range []app.TextEvent{
				{Kind: app.TextSetMarked, Text: "x\ny", MarkedSelection: app.TextRange{Start: 3, End: 3}},
				{Kind: ending, Text: "x\ny"},
			} {
				if _, err := state.onHTMLTextEvent(event); err != nil {
					context.Fatal(err)
				}
			}
			current := state.session.Current()
			if current.Text() != original || current.Dirty() || current.Editor.CanUndo() || state.html.input.CommittedString() != "abc" || len(state.session.RecoverySnapshot().Drafts) != 0 || state.message == "" {
				context.Fatalf("rejected commit left uncommitted input: source=%q native=%q message=%q", current.Text(), state.html.input.CommittedString(), state.message)
			}
			state.saveActive(false)
			if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, []byte(original)) {
				context.Fatalf("rejected commit reached disk: %q / %v", data, err)
			}
		})
	}
}
