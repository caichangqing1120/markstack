package main

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

func TestM13CodeInsertMouseAndSlashEntrypoints(t *testing.T) {
	for _, entry := range []string{"toolbar", "paragraph menu", "slash"} {
		t.Run(entry, func(t *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(t, "", false, 1200)
			switch entry {
			case "toolbar":
				clickID(t, driver, formatCodeBlockID)
			case "paragraph menu":
				clickID(t, driver, paragraphMenuID)
				clickID(t, driver, paragraphItems.At(11))
			case "slash":
				m13Type(t, driver, "/")
				m13Type(t, driver, "code")
				upgradeKey(t, driver, gfx.KeyEnter, 0)
			}
			if state.mode != renderedMode || !state.codeBlock.active || driver.Snapshot().Focused != codeInputID || state.codeBlock.input.Caret() != 0 {
				t.Fatalf("creation did not focus code at 1:1: mode=%v focus=%v", state.mode, driver.Snapshot().Focused)
			}
			m13Type(t, driver, "print(1)")
			if !strings.Contains(strings.ReplaceAll(state.session.Current().Text(), "\r\n", "\n"), "```\nprint(1)\n```") {
				t.Fatalf("first keystrokes left the fence: %q", state.session.Current().Text())
			}
		})
	}
}

func TestM13LanguagePickerScrollDismissPlainAndCustom(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "en"
	const original = "```go\npackage main\n```\n\nTail\n"
	state.session.Current().Replace(original)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, codeLanguageButtons.At(0))
	options := state.fenceLanguageSuggestions()
	if len(options) < 49 || strings.Join(options[:7], ",") != "plaintext,go,python,sql,javascript,typescript,java" || !m12Hit(t, driver, codeLanguageSuggestions.At(1)).Selected {
		t.Fatalf("picker did not pin languages or mark current: %v", options)
	}
	viewport := state.codeBlock.suggestionViewport
	pageScroll := state.previewScroll
	for _, delta := range []float32{20, 2000} {
		if err := driver.Scroll(gfx.ScrollEvent{X: viewport.X + 2, Y: viewport.Y + 2, DeltaY: delta, Precise: true}); err != nil {
			t.Fatal(err)
		}
	}
	if state.codeBlock.suggestionScroll == 0 || state.previewScroll != pageScroll || driver.Snapshot().Focused != codeLanguageInputID {
		t.Fatal("wheel did not scroll picker independently")
	}
	m12Hit(t, driver, codeLanguageSuggestions.At(len(options)-1))
	clickID(t, driver, editModeID)
	if state.mode != renderedMode || state.codeBlock.languageInput != nil || driver.Snapshot().Focused != codeInputID || state.session.Current().Text() != original {
		t.Fatal("outside click did not dismiss without triggering the background control")
	}
	clickID(t, driver, codeLanguageButtons.At(0))
	m13Type(t, driver, "unused-custom.tag")
	upgradeKey(t, driver, gfx.KeyEscape, 0)
	if state.session.Current().Text() != original || driver.Snapshot().Focused != codeInputID {
		t.Fatal("Escape applied a pending search")
	}
	clickID(t, driver, codeLanguageButtons.At(0))
	m13Type(t, driver, "custom tag")
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	if state.session.Current().Text() != original || driver.Snapshot().Focused != codeLanguageInputID {
		t.Fatal("invalid fence tag was accepted")
	}
	m1Shortcut(t, driver, primaryModifier(), 'a')
	m13Type(t, driver, "unused-custom.tag")
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	if got := state.session.Current().Text(); got != strings.Replace(original, "```go", "```unused-custom.tag", 1) {
		t.Fatalf("custom tag = %q", got)
	}
	clickID(t, driver, codeLanguageButtons.At(0))
	m13Type(t, driver, "纯文本")
	clickID(t, driver, codeLanguageSuggestions.At(0))
	if got := state.session.Current().Text(); got != strings.Replace(original, "```go", "```", 1) || driver.Snapshot().Focused != codeInputID {
		t.Fatalf("plain text did not clear the fence: %q", got)
	}
	m1Shortcut(t, driver, primaryModifier(), 'z')
	if state.codeBlock.block.Code.Info != "unused-custom.tag" {
		t.Fatal("one Undo did not restore the previous language")
	}
	m1Shortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'z')
	if state.codeBlock.block.Code.Info != "" {
		t.Fatal("one Redo did not clear the language again")
	}
}

func TestM13HeaderTogglesCopyFeedbackAndExit(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "zh-CN"
	line := strings.Repeat("example() ", 32)
	source := "```js\n" + line + "\n```\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if !m12Hit(t, driver, codeLineNumberButtons.At(0)).Checked || !m12Hit(t, driver, codeWrapButtons.At(0)).Checked || state.codeBlock.view.ScrollY() <= 0 {
		t.Fatal("line numbers and wrap did not start on")
	}
	clickID(t, driver, codeLineNumberButtons.At(0))
	clickID(t, driver, codeWrapButtons.At(0))
	if !state.preferences.HideCodeLineNumbers || !state.preferences.DisableCodeWrap || state.codeBlock.view.ScrollY() != 0 || state.codeBlock.view.ScrollX() <= 0 || m12Hit(t, driver, codeLineNumberButtons.At(0)).Checked || m12Hit(t, driver, codeWrapButtons.At(0)).Checked {
		t.Fatal("header toggles did not update the editor and their checked state")
	}
	if state.session.Current().Text() != source || driver.Snapshot().Focused != codeInputID {
		t.Fatal("toggling presentation options changed the document or focus")
	}
	clickID(t, driver, codeCopyButtons.At(0))
	if !upgradeExactLabel(driver, "✓ 已复制") || driver.Clipboard() != line {
		t.Fatalf("copy feedback or copied text is wrong: %q", driver.Clipboard())
	}
	if err := driver.Advance(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !upgradeExactLabel(driver, "复制") || upgradeExactLabel(driver, "✓ 已复制") {
		t.Fatal("copy feedback did not expire")
	}
	clickID(t, driver, codeExitButtons.At(0))
	if !state.rich.active || state.rich.input.String() != "" || state.codeBlock.active || driver.Snapshot().Focused != richInputID {
		t.Fatal("exit button did not focus a new empty paragraph")
	}
	m13Type(t, driver, "below")
	if got := state.session.Current().Text(); got != source+"\nbelow" {
		t.Fatalf("paragraph typing landed at %q", got)
	}
}

func TestM13ExitAndBackspaceKeepSingleStepCRLFHistory(t *testing.T) {
	for _, scenario := range []struct {
		name, source, edited, typed string
		key                         gfx.Key
	}{
		{"non-text next block", "```go\nbody\n```\n---\n", "```go\nbody\n```\n\n\n\n---\n", "```go\nbody\n```\n\nnext\n\n---\n", gfx.KeyDown},
		{"empty fence between paragraphs", "Before\n\n```\n\n```\n\nAfter\n", "Before\n\n\n\nAfter\n", "Before\n\nnext\n\nAfter\n", gfx.KeyBackspace},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state, driver, path, original := renderedSourceCollisionFixture(t, scenario.source, false, 1200)
			index := 0
			if scenario.key == gfx.KeyBackspace {
				index = 1
			}
			clickID(t, driver, renderedRows.At(index))
			upgradeKey(t, driver, scenario.key, 0)
			if !state.rich.active || state.rich.input.String() != "" || driver.Snapshot().Focused != richInputID {
				t.Fatal("did not create a paragraph input")
			}
			expectRenderedCollisionSave(t, state, driver, path, m1Saved(scenario.edited))
			m1Shortcut(t, driver, primaryModifier(), 'z')
			if !state.codeBlock.active || driver.Snapshot().Focused != codeInputID {
				t.Fatal("Undo did not restore the code input")
			}
			expectRenderedCollisionSave(t, state, driver, path, original)
			m1Shortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'z')
			if !state.rich.active || driver.Snapshot().Focused != richInputID {
				t.Fatal("Redo did not restore the paragraph input")
			}
			m13Type(t, driver, "next")
			expectRenderedCollisionSave(t, state, driver, path, m1Saved(scenario.typed))
		})
	}
}

func TestM13AutoIndentCRLFAndCompositionSafety(t *testing.T) {
	state, driver, path, original := renderedSourceCollisionFixture(t, "```python\n\tif 我:\n```\n", false, 1200)
	clickID(t, driver, renderedRows.At(0))
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	expectRenderedCollisionSave(t, state, driver, path, m1Saved("```python\n\tif 我:\n\t\t\n```\n"))
	m1Shortcut(t, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(t, state, driver, path, original)
	m1Shortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'z')
	if state.codeBlock.input.String() != "\tif 我:\n\t\t" || state.codeBlock.input.Caret() != state.codeBlock.input.RuneCount() {
		t.Fatal("auto-indent Redo lost the new indentation or caret")
	}
	before := state.session.Current().Text()
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, codeExitButtons.At(0))
	clickID(t, driver, codeWrapButtons.At(0))
	upgradeKey(t, driver, gfx.KeyEnter, primaryModifier())
	if !state.codeBlockComposing() || !state.codeBlock.active || state.preferences.DisableCodeWrap || state.session.Current().Text() != before || driver.Snapshot().Focused != codeInputID {
		t.Fatal("a code command interrupted IME or wrote the preedit")
	}
	m13Type(t, driver, "你")
	input := state.codeBlock.input
	input.SetSelection(text.Range{Start: input.RuneCount() - 1, End: input.RuneCount()})
	upgradeKey(t, driver, gfx.KeyDown, 0)
	if !state.codeBlock.active || driver.Snapshot().Focused != codeInputID {
		t.Fatal("Down exited while code was selected")
	}
}

func m13Type(t *testing.T, driver *app.Driver, value string) {
	t.Helper()
	if err := driver.Type(value); err != nil {
		t.Fatal(err)
	}
}

func TestM13FenceTriggerFocusesEmphasizedCodeCard(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "zh-CN"
	state.session.Current().Replace("\n")
	clickID(t, driver, renderedModeID)
	if err := driver.Type("```"); err != nil {
		t.Fatal(err)
	}
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	frame := driver.Snapshot()
	if state.mode != renderedMode || !state.codeBlock.active || state.codeBlock.input.Caret() != 0 || frame.Focused != codeInputID {
		t.Fatalf("fence trigger: mode=%v active=%v focus=%v", state.mode, state.codeBlock.active, frame.Focused)
	}
	if !upgradeExactLabel(driver, "选择语言… ▾") {
		t.Fatal("empty-language pill is missing")
	}
	_, palette := state.look()
	left, width := state.markdownColumn(state.previewRect)
	for _, primitive := range frame.Primitives {
		if primitive.Color == palette.accent && primitive.Bounds.X == left-2 && primitive.Bounds.Width == width+4 && primitive.Bounds.Height > codeHeaderHeight {
			return
		}
	}
	t.Fatal("active card is missing its full accent ring")
}

func TestM13LanguagePickerSearchAndKeyboard(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```\nSELECT 1;\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	keywordColor := func() gfx.Color {
		frame := driver.Snapshot()
		for _, label := range frame.Texts {
			if label.Value == "SELECT 1;" {
				for _, run := range frame.TextRuns(label) {
					if run.Column == 0 {
						return run.Color
					}
				}
				return label.Style.Color
			}
		}
		t.Fatal("code body did not paint")
		return gfx.Color{}
	}
	plainColor := keywordColor()
	clickID(t, driver, codeLanguageButtons.At(0))
	if driver.Snapshot().Focused != codeLanguageInputID || state.codeBlock.languageInput.String() != "" {
		t.Fatal("picker did not focus empty search")
	}
	if err := driver.Type("pg"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, codeLanguageSuggestions.At(0))
	if !strings.HasPrefix(state.session.Current().Text(), "```postgresql\n") || driver.Snapshot().Focused != codeInputID {
		t.Fatal("mouse selection did not apply PostgreSQL")
	}
	if keywordColor() == plainColor || !upgradeExactLabel(driver, "PostgreSQL ▾") {
		t.Fatal("selected language did not repaint the badge and syntax highlighting")
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierAlt, 'l'); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != codeLanguageInputID || state.codeBlock.languageInput.String() != "" {
		t.Fatal("shortcut did not reopen empty search")
	}
	if err := driver.Type("script"); err != nil {
		t.Fatal(err)
	}
	upgradeKey(t, driver, gfx.KeyDown, 0)
	upgradeKey(t, driver, gfx.KeyUp, 0)
	upgradeKey(t, driver, gfx.KeyDown, 0)
	if driver.Snapshot().Focused != codeLanguageInputID {
		t.Fatal("arrows moved focus away from search")
	}
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	if !strings.HasPrefix(state.session.Current().Text(), "```typescript\n") || driver.Snapshot().Focused != codeInputID {
		t.Fatal("keyboard selection did not apply TypeScript")
	}
	if options := workspace.CodeLanguageSuggestions("ts", 50); len(options) == 0 || options[0] != "typescript" {
		t.Fatalf("TypeScript alias returned %v", options)
	}
}

func TestM13PlainTextKeepsFenceOptionsAndSingleStepHistory(t *testing.T) {
	const source = "```go title=demo\npackage main\n```\n"
	state, driver, path, original := renderedSourceCollisionFixture(t, source, false, 1200)
	clickID(t, driver, codeLanguageButtons.At(0))
	clickID(t, driver, codeLanguageSuggestions.At(0))
	want := m1Saved(strings.Replace(source, "```go", "```", 1))
	expectRenderedCollisionSave(t, state, driver, path, want)
	m1Shortcut(t, driver, primaryModifier(), 'z')
	expectRenderedCollisionSave(t, state, driver, path, original)
	m1Shortcut(t, driver, primaryModifier()|gfx.ModifierShift, 'z')
	expectRenderedCollisionSave(t, state, driver, path, want)
	if !state.codeBlock.active || driver.Snapshot().Focused != codeInputID {
		t.Fatal("language history lost the code input")
	}
}

func TestM13CodeHeaderFitsNarrowPane(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```\n\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for _, width := range []int{1200, 540, 450} {
		if err := driver.Resize(width, 520); err != nil {
			t.Fatal(err)
		}
		_, column := state.markdownColumn(state.previewRect)
		ids := []gfx.ID{codeLanguageButtons.At(0), codeLineNumberButtons.At(0), codeWrapButtons.At(0), codeExitButtons.At(0), codeCopyButtons.At(0)}
		last := float32(-1)
		for index, id := range ids {
			found := false
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID == id {
					found = true
					if hit.Bounds.X < last {
						t.Fatalf("code header controls overlap at column %.1f", column)
					}
					last = hit.Bounds.X + hit.Bounds.Width
					break
				}
			}
			want := column >= 280 || index != 1 && index != 2
			if found != want {
				t.Fatalf("column %.1f: control %v visible=%v, want %v", column, id, found, want)
			}
		}
	}
}

func TestM13CodeEnterExitAndEmptyBackspace(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\n    if ready {\n```\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	upgradeKey(t, driver, gfx.KeyEnter, 0)
	if got := state.codeBlock.input.String(); got != "    if ready {\n        " {
		t.Fatalf("auto-indent body = %q", got)
	}
	upgradeKey(t, driver, gfx.KeyEnter, primaryModifier())
	if driver.Snapshot().Focused != richInputID || state.codeBlock.active || state.rich.input.Caret() != 0 || state.rich.input.String() != "Tail" {
		t.Fatal("Primary+Enter did not focus the next paragraph at its start")
	}
	state.session.Current().Replace("```\n\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	upgradeKey(t, driver, gfx.KeyBackspace, 0)
	if state.codeBlock.active || driver.Snapshot().Focused != richInputID || strings.Contains(state.session.Current().Text(), "```") {
		t.Fatal("Backspace did not replace the empty fence with a paragraph")
	}
}
