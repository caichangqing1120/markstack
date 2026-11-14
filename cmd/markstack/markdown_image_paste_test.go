package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func TestMarkdownSourceImagePastePreservesBOMCRLFAndRelocates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	source := "# 标题\r\n\r\n中文正文\r\n"
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	before := "# 标题\r\n\r\n中文"
	state.session.Current().Editor.MoveTo(utf8.RuneCountInString(before), false)
	imageBytes := imagePNG(t, 2, 1)
	if err := driver.SetClipboardImage(imageBytes); err != nil {
		t.Fatal(err)
	}
	if handled, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil || !handled {
		t.Fatalf("paste image handled=%v err=%v", handled, err)
	}
	reference := "![图片](" + workspace.PNGAssetPath(imageBytes) + ")"
	want := before + reference + "正文\r\n"
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("image source = %q, message=%q", got, state.message)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != source {
		t.Fatalf("paste undo = %q, %v", state.session.Current().Text(), err)
	}
	// Undo deliberately leaves the content-addressed asset on disk: resource
	// lifecycle changes are a separate product decision, never a silent delete.
	if _, err := os.Stat(filepath.Join(root, workspace.PNGAssetPath(imageBytes))); err != nil {
		t.Fatalf("undo silently removed the pasted asset: %v", err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != want {
		t.Fatalf("paste redo = %q, %v", state.session.Current().Text(), err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "\xef\xbb\xbf"+want {
		t.Fatalf("saved markdown bytes=%q, %v", data, err)
	}
	assetPath := filepath.Join(root, workspace.PNGAssetPath(imageBytes))
	savedAsset, err := os.ReadFile(assetPath)
	if err != nil || !bytes.Equal(savedAsset, imageBytes) {
		t.Fatalf("saved asset=%d bytes, %v", len(savedAsset), err)
	}
	originalHash := sha256.Sum256(savedAsset)
	// The same image pasted again is reused through its content-addressed path
	// instead of duplicating a second asset.
	clickID(t, driver, sourceID)
	state.session.Current().Editor.MoveTo(utf8.RuneCountInString(want), false)
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	want = want + reference
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("second paste = %q, message=%q", got, state.message)
	}
	entries, err := os.ReadDir(filepath.Join(root, "assets"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("same image duplicated assets: %+v, %v", entries, err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "\xef\xbb\xbf"+want {
		t.Fatalf("reused-image save=%q, %v", data, err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(moved, "note.md")
	reopened := session.New()
	if _, err := reopened.Open(newPath); err != nil {
		t.Fatal(err)
	}
	movedAsset, err := os.ReadFile(filepath.Join(moved, workspace.PNGAssetPath(imageBytes)))
	if err != nil || sha256.Sum256(movedAsset) != originalHash {
		t.Fatalf("moved asset hash changed: %v", err)
	}
	asset, err := workspace.ReadAsset("", newPath, reopened.Current().SavedLocation(), workspace.PNGAssetPath(imageBytes))
	if err != nil || !bytes.Equal(asset, imageBytes) || reopened.Current().Text() != want {
		t.Fatalf("relocated image=%d bytes source=%q err=%v", len(asset), reopened.Current().Text(), err)
	}
	if err := reopened.Save(reopened.Active, ""); err != nil {
		t.Fatalf("relocated no-op save: %v", err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "\xef\xbb\xbf"+want {
		t.Fatalf("relocated save=%q, %v", data, err)
	}
}

func TestMarkdownImagePasteRefusesUnsavedStaleAndSymlinkAssets(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if err := driver.SetClipboardImage(imagePNG(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "" || state.message == "" {
		t.Fatalf("unsaved paste source=%q message=%q", state.session.Current().Text(), state.message)
	}
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	if err := os.WriteFile(path, []byte("outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "before\n" {
		t.Fatal("stale document changed in memory")
	}
	if _, err := os.Stat(filepath.Join(root, "assets")); !os.IsNotExist(err) {
		t.Fatalf("stale paste created assets: %v", err)
	}
	state, driver = testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}
	if err := driver.SetClipboardImage(imagePNG(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || !strings.Contains(state.message, "workspace") {
		t.Fatalf("symlink paste dirty=%v message=%q", state.session.Current().Dirty(), state.message)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside modified: %v, %v", entries, err)
	}
}

func TestRenderedMarkdownImagePasteOnlyUsesSimpleSourcePoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("Simple paragraph\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(6, false)
	imageBytes := imagePNG(t, 1, 1)
	if err := driver.SetClipboardImage(imageBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "Simple"+"![图片]("+workspace.PNGAssetPath(imageBytes)+")"+" paragraph\n" {
		t.Fatalf("rendered paste source=%q message=%q", got, state.message)
	}
	if state.mode != editMode {
		t.Fatal("unrenderable image paste was not handed to explicit source mode")
	}
}

func TestMarkdownSourceImageHandlerKeepsTextPasteAndIME(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("普通")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if _, composing := state.session.Current().Editor.Composition(); !composing || state.session.Current().Text() != "普通pin" {
		t.Fatalf("IME preedit state: text=%q composing=%v", state.session.Current().Text(), composing)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "普通新" {
		t.Fatalf("native text route = %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != "普通" {
		t.Fatalf("native text undo = %q, %v", state.session.Current().Text(), err)
	}
}

func TestMarkdownSourceImagePasteRejectsOversizedPNGAndCanReplaceUnicodeSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("中文正文\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	corrupt := imagePNG(t, 2, 2)[:33]
	if err := driver.SetClipboardImage(corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || state.message == "" {
		t.Fatalf("corrupt image changed source: dirty=%v message=%q", state.session.Current().Dirty(), state.message)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "assets")); !os.IsNotExist(err) {
		t.Fatalf("corrupt image created assets: %v", err)
	}
	if err := driver.SetClipboardImage(imagePNG(t, 5000, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || state.message == "" {
		t.Fatalf("oversized image changed source: dirty=%v message=%q", state.session.Current().Dirty(), state.message)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "assets")); !os.IsNotExist(err) {
		t.Fatalf("oversized image created assets: %v", err)
	}
	imageBytes := imagePNG(t, 1, 1)
	if err := driver.SetClipboardImage(imageBytes); err != nil {
		t.Fatal(err)
	}
	state.session.Current().Editor.SetSelection(text.Range{Start: 2, End: 4})
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "中文![图片]("+workspace.PNGAssetPath(imageBytes)+")\n" {
		t.Fatalf("Unicode selection paste = %q", got)
	}
}

func TestRenderedMarkdownImagePasteRefusesStyledSourceWithoutAsset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("**bold** text\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SetClipboardImage(imagePNG(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || state.message == "" {
		t.Fatalf("styled paste dirty=%v message=%q", state.session.Current().Dirty(), state.message)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "assets")); !os.IsNotExist(err) {
		t.Fatalf("styled paste created assets: %v", err)
	}
}
