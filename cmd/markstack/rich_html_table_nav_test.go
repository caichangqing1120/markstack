package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/richedit"
)

func openTableNavFixture(t *testing.T, source string) (*model, *app.Driver) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "table.mh")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	return state, driver
}

func TestRichHTMLTableTabNavigatesWithoutWritingTabs(t *testing.T) {
	source := "<table><tbody><tr><th>列</th><td>A</td></tr><tr><td>B</td><td>C</td></tr></tbody></table>\n<!--keep-->"
	state, driver := openTableNavFixture(t, source)
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if !state.html.active || state.html.blockIndex != 1 || state.html.input.String() != "A" || state.session.Current().Dirty() {
		t.Fatalf("Tab did not focus next cell: block=%d input=%q dirty=%v", state.html.blockIndex, state.html.input.String(), state.session.Current().Dirty())
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if state.html.blockIndex != 0 || state.html.input.String() != "列" || strings.Contains(state.session.Current().Text(), "\t") {
		t.Fatalf("Shift+Tab changed source or missed previous cell: block=%d source=%q", state.html.blockIndex, state.session.Current().Text())
	}
	if want := strings.Replace(source, "<td>A</td>", "<td>AX</td>", 1); state.session.Current().Text() != want {
		t.Fatalf("Tab navigation changed unrelated HTML: %q", state.session.Current().Text())
	}
}

func TestRichHTMLLastTableTabAddsRowAndUndoRestoresBytes(t *testing.T) {
	source := "<table><tbody><tr><td>A</td><td>B</td></tr></tbody></table>\n<!--keep-->"
	state, driver := openTableNavFixture(t, source)
	clickID(t, driver, htmlLeafRows.At(1))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	blank := "<table><tbody><tr><td>A</td><td>B</td></tr>\n<tr><td><br></td><td><br></td></tr></tbody></table>\n<!--keep-->"
	if state.session.Current().Text() != blank || state.html.blockIndex != 2 || !state.html.active || state.html.input.String() != "" {
		t.Fatalf("Tab did not create editable row: block=%d source=%q", state.html.blockIndex, state.session.Current().Text())
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.session.Current().Text(), "<tr><td>新</td><td><br></td></tr>") {
		t.Fatalf("new cell input missed: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != blank {
		t.Fatalf("first undo lost row: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("second undo did not restore exact source: %q dirty=%v", state.session.Current().Text(), state.session.Current().Dirty())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != blank {
		t.Fatalf("redo did not restore row: %q", state.session.Current().Text())
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(state.session.Current().Path); err != nil || string(data) != blank {
		t.Fatalf("saved table row=%q, %v", data, err)
	}
}

func TestRealMkDownTableTabKeepsCanonicalAttributesAndHistory(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "internal", "richedit", "testdata", "Real-MkDown-Structure.mh"))
	if err != nil {
		t.Fatal(err)
	}
	state, driver := openTableNavFixture(t, string(original))
	lastIndex := -1
	for index, block := range state.html.parsed.Blocks {
		if block.Kind == richedit.TableCell && block.Visible == "值 & 保留" {
			lastIndex = index
		}
	}
	if lastIndex < 0 {
		t.Fatal("real MkDown cell was not rendered")
	}
	clickID(t, driver, htmlLeafRows.At(lastIndex))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	insert := "\n<tr><td colSpan=\"1\" rowSpan=\"1\" width=\"auto\"><br></td><td colSpan=\"1\" rowSpan=\"1\" width=\"auto\"><br></td></tr>"
	blank := strings.Replace(string(original), "</tr></tbody></table>", "</tr>"+insert+"</tbody></table>", 1)
	if state.session.Current().Text() != blank || state.html.blockIndex != lastIndex+1 || !state.html.active || state.html.input.String() != "" {
		t.Fatalf("real MkDown Tab did not make an editable row: block=%d source=%q", state.html.blockIndex, state.session.Current().Text())
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.session.Current().Text(), `<td colSpan="1" rowSpan="1" width="auto">新</td>`) {
		t.Fatalf("real MkDown new cell input lost attributes: %q", state.session.Current().Text())
	}
	for range 2 {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
	}
	if state.session.Current().Text() != string(original) || state.session.Current().Dirty() {
		t.Fatalf("real MkDown undo changed original source: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != blank {
		t.Fatalf("real MkDown redo = %q", state.session.Current().Text())
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(state.session.Current().Path); err != nil || string(saved) != blank {
		t.Fatalf("real MkDown saved row=%q, %v", saved, err)
	}
}

func TestRichHTMLUnsafeTableTabDoesNotInsertLiteralTab(t *testing.T) {
	source := "<table><tbody><tr><td colspan=\"2\">A</td></tr></tbody></table>\n<!--keep-->"
	state, driver := openTableNavFixture(t, source)
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() || state.message == "" {
		t.Fatalf("unsafe Tab mutated HTML: source=%q message=%q", state.session.Current().Text(), state.message)
	}
}

func TestRichHTMLTableTabDoesNotCrossAdjacentTable(t *testing.T) {
	source := "<table><tr><td>A</td></tr></table><table><tr><td>B</td></tr></table>"
	state, driver := openTableNavFixture(t, source)
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if state.html.blockIndex != 1 || state.html.parsed.Blocks[1].TableStart != 0 || !strings.HasSuffix(state.session.Current().Text(), "<table><tr><td>B</td></tr></table>") {
		t.Fatalf("Tab crossed into second table: block=%d source=%q", state.html.blockIndex, state.session.Current().Text())
	}
}

func TestRichHTMLTableTabWaitsForComposition(t *testing.T) {
	source := "<table><tr><td>A</td><td>B</td></tr></table>"
	state, driver := openTableNavFixture(t, source)
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if state.html.blockIndex != 0 || state.session.Current().Text() != source || state.session.Current().Dirty() || !strings.Contains(state.message, "输入法") {
		t.Fatalf("Tab committed preedit: block=%d source=%q message=%q", state.html.blockIndex, state.session.Current().Text(), state.message)
	}
}
