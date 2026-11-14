package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/workspace"
)

func TestRichHTMLPasteClipboardImageWritesLocalAssetAndUndoesMarkup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.mh")
	source := `<p>A &amp; B</p><!--keep--><x-unknown>raw</x-unknown>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	state.html.input.MoveTo(utf8.RuneCountInString("A & "), false)
	imageBytes := imagePNG(t, 2, 1)
	if err := driver.SetClipboardImage(imageBytes); err != nil {
		t.Fatal(err)
	}
	if handled, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil || !handled {
		t.Fatalf("paste image handled=%v err=%v", handled, err)
	}
	updated := state.session.Current().Text()
	if !strings.HasPrefix(updated, `<p>A &amp; <img src="assets/`) || !strings.HasSuffix(updated, `" alt="">B</p><!--keep--><x-unknown>raw</x-unknown>`) {
		t.Fatalf("image paste source = %q, message=%q", updated, state.message)
	}
	image := state.html.parsed.Blocks[0].Leaves[1]
	data, err := workspace.ReadAsset("", path, state.session.Current().SavedLocation(), image.ImageSource)
	if err != nil || !bytes.Equal(data, imageBytes) {
		t.Fatalf("pasted asset = %d bytes, %v", len(data), err)
	}
	if len(canvas.Images()) != 1 {
		t.Fatal("pasted image was not drawn")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != source {
		t.Fatalf("image undo = %q, %v", state.session.Current().Text(), err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != updated {
		t.Fatalf("image redo = %q, %v", state.session.Current().Text(), err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != updated {
		t.Fatalf("saved HTML = %q, %v", saved, err)
	}
}

func TestRichHTMLImagePasteRequiresSavedUnchangedDocument(t *testing.T) {
	state, driver := testDriver(t)
	state.newRichHTML()
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	before := state.session.Current().Text()
	if err := driver.SetClipboardImage(imagePNG(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != before || state.message == "" {
		t.Fatalf("unsaved rich paste source=%q message=%q", state.session.Current().Text(), state.message)
	}

	path := filepath.Join(t.TempDir(), "changed.mh")
	if err := os.WriteFile(path, []byte("<p>old</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := os.WriteFile(path, []byte("<p>external</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "<p>old</p>" || state.message == "" {
		t.Fatalf("stale rich paste source=%q message=%q", state.session.Current().Text(), state.message)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "assets")); !os.IsNotExist(err) {
		t.Fatalf("stale paste created asset directory: %v", err)
	}
}

func TestRichHTMLPasteImageAtParagraphEndKeepsTypingAfterImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "end.mh")
	if err := os.WriteFile(path, []byte("<p>A</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.SetClipboardImage(imagePNG(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("Z"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); !strings.HasPrefix(got, `<p>A<img src="assets/`) || !strings.HasSuffix(got, `" alt="">Z</p>`) {
		t.Fatalf("typing after pasted image = %q", got)
	}
	withText := state.session.Current().Text()
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || strings.HasSuffix(state.session.Current().Text(), `Z</p>`) {
		t.Fatalf("typing undo = %q, %v", state.session.Current().Text(), err)
	}
	withoutText := state.session.Current().Text()
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != "<p>A</p>" {
		t.Fatalf("image undo = %q, %v", state.session.Current().Text(), err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != withoutText {
		t.Fatalf("image redo = %q, %v", state.session.Current().Text(), err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != withText {
		t.Fatalf("typing redo = %q, %v", state.session.Current().Text(), err)
	}
}

func TestRichHTMLPasteImageIntoEmptyParagraphKeepsCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.mh")
	source := "<p><br></p>\r\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.SetClipboardImage(imagePNG(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); !strings.HasPrefix(got, `<p><img src="assets/`) || !strings.HasSuffix(got, `" alt="">新</p>`+"\r\n<!--keep-->") {
		t.Fatalf("empty rich image insertion = %q, message=%q", got, state.message)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != state.session.Current().Text() {
		t.Fatalf("saved image paragraph = %q, %v", saved, err)
	}
}
