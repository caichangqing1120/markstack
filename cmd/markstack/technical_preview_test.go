package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
)

func TestTechnicalDriverEditAndSingleUndo(t *testing.T) {
	for _, fixture := range []struct {
		name, source, body, before, after string
		reading                           bool
	}{
		{"math", "$$\n\\frac{x}{b}\n$$\n", `\frac{x}{b}`, "x", "n", true},
		{"single-line", "$$x^2$$\n", "x^2", "x", "n", false},
		{"mermaid", "```mermaid\ngraph LR\nA[before] --> B{Done}\n```\n", "graph LR\nA[before] --> B{Done}", "before", "after", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := "Price \\$100 and $$200.\n\n" + fixture.source + "\nTail `unchanged`.\n"
			state, driver, path, original := renderedSourceCollisionFixture(t, source, fixture.reading, 1200)
			if len(state.preview.blocks) != 3 || state.preview.blocks[1].vector.Width == 0 || !hasText(driver, fixture.before) {
				t.Fatalf("native vector missing: %+v", state.preview.displayMath)
			}
			if fixture.reading {
				clickID(t, driver, readModeID)
				clickID(t, driver, technicalPreviewRows.At(1))
			} else {
				clickID(t, driver, renderedRows.At(1))
			}
			if !state.codeBlock.active || state.codeBlock.input.String() != fixture.body {
				t.Fatal("click did not activate the exact source body")
			}
			at := strings.Index(fixture.body, fixture.before)
			state.codeBlock.input.SetSelection(text.Range{Start: at, End: at + len(fixture.before)})
			if err := driver.Type(fixture.after); err != nil {
				t.Fatal(err)
			}
			if !hasText(driver, fixture.after) || !state.codeBlock.active || state.preview.blocks[1].vector.Width == 0 {
				t.Fatal("editing did not update the live vector")
			}
			want := []byte(strings.Replace(string(original), fixture.before, fixture.after, 1))
			checkTechnicalSaved(t, state, driver, path, want)
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			checkTechnicalSaved(t, state, driver, path, original)
			if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
				t.Fatal(err)
			}
			checkTechnicalSaved(t, state, driver, path, want)
		})
	}
}

func TestTechnicalDriverInlineSourceAndFallback(t *testing.T) {
	const source = "Alpha $\\frac{x}{b}$ omega.\n\n$$\n\\unknown{x}\n$$\n\n```mermaid\npie\nA: 4\n```\n"
	state, driver, path, original := renderedSourceCollisionFixture(t, source, false, 1200)
	if len(state.preview.blocks) != 3 || len(state.preview.blocks[0].technical) == 0 || !state.preview.blocks[1].mathFallback || state.preview.blocks[2].vector.Width != 0 || !hasText(driver, "不支持的公式") || !hasText(driver, "pie") {
		t.Fatal("expected inline vector, LaTeX diagnostic and source-only Mermaid")
	}
	clickID(t, driver, renderedRows.At(0))
	if !state.rich.active || state.rich.input.String() != `Alpha $\frac{x}{b}$ omega.` || len(state.preview.blocks[0].technical) != 0 {
		t.Fatal("active paragraph did not reveal original formula source")
	}
	start := strings.Index(state.rich.input.String(), "x")
	state.rich.input.SetSelection(text.Range{Start: start, End: start + 1})
	if err := driver.Type("n"); err != nil {
		t.Fatal(err)
	}
	want := []byte(strings.Replace(string(original), "{x}", "{n}", 1))
	checkTechnicalSaved(t, state, driver, path, want)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	checkTechnicalSaved(t, state, driver, path, original)
	clickID(t, driver, readModeID)
	if len(state.preview.blocks[0].technical) == 0 {
		t.Fatal("leaving the editor did not restore the inline vector")
	}
}

func checkTechnicalSaved(t *testing.T, state *model, driver *app.Driver, path string, want []byte) {
	t.Helper()
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, want) || state.session.Current().Dirty() {
		t.Fatalf("saved %q, want %q, error=%v, message=%s", actual, want, err, state.message)
	}
}
