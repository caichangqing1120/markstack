package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"

	"markstack/internal/imagehost"
	"markstack/internal/workspace"
)

func TestImageHostPasteRemoteIsUndoable(t *testing.T) {
	state, driver, path, bitmap := imageHostPasteFixture(t)
	started, finish := make(chan struct{}), make(chan struct{})
	state.imageHost.upload = func(ctx context.Context, cfg imagehost.Config, filename string, body []byte, contentType string) (string, error) {
		close(started)
		select {
		case <-finish:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if cfg.Mode != imagehost.ModeHTTP || filename != "image.png" || contentType != "image/png" || string(body) != string(bitmap) {
			return "", errors.New("wrong upload")
		}
		return "https://cdn.example/a(1).png", nil
	}
	if err := driver.SelectText(1, 2); err != nil {
		t.Fatal(err)
	}
	pasteImageHostClipboard(t, driver)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upload did not start")
	}
	doc := state.session.Current()
	if doc.Dirty() || doc.Text() != "甲乙丙\r\n" {
		t.Fatal("pending network request modified the document")
	}
	close(finish)
	waitImageHost(t, driver, func() bool { return state.imageHost.paste == nil })
	const want = "甲![image](<https://cdn.example/a(1).png>)丙\r\n"
	if doc.Text() != want {
		t.Fatalf("unexpected upload edit: %q", doc.Text())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "assets")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("remote paste unexpectedly wrote local assets")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || doc.Text() != "甲乙丙\r\n" {
		t.Fatalf("remote paste did not undo in a single step: %v", err)
	}
}

func TestImageHostPasteFailureRetainsImageForExplicitFallback(t *testing.T) {
	state, driver, path, bitmap := imageHostPasteFixture(t)
	state.imageHost.upload = func(context.Context, imagehost.Config, string, []byte, string) (string, error) {
		return "", errors.New("remote response includes private-token")
	}
	pasteImageHostClipboard(t, driver)
	waitImageHost(t, driver, func() bool { return state.imageHost.paste != nil && state.imageHost.paste.scan.ready == nil })
	doc := state.session.Current()
	asset := filepath.Join(filepath.Dir(path), workspace.PNGAssetPath(bitmap))
	if _, err := os.Stat(asset); !errors.Is(err, os.ErrNotExist) || doc.Dirty() || !hasText(driver, "Upload failed") || hasText(driver, "private-token") {
		t.Fatal("failure changed the document or assets, did not show feedback, or exposed private data")
	}
	if err := driver.SetClipboardImage(imagePNG(t, 3, 1)); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, imagePasteActionID)
	if data, err := os.ReadFile(asset); err != nil || string(data) != string(bitmap) || doc.Text() != "![图片]("+workspace.PNGAssetPath(bitmap)+")甲乙丙\r\n" {
		t.Fatalf("fallback did not use original image: %q, %v", doc.Text(), err)
	}
}

func TestImageHostPasteDoesNotOverwriteTypingDuringUpload(t *testing.T) {
	state, driver, _, _ := imageHostPasteFixture(t)
	finish := make(chan struct{})
	state.imageHost.upload = func(ctx context.Context, cfg imagehost.Config, filename string, data []byte, contentType string) (string, error) {
		select {
		case <-finish:
			return "https://cdn.example/a.png", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	pasteImageHostClipboard(t, driver)
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	close(finish)
	waitImageHost(t, driver, func() bool { return state.imageHost.paste != nil && state.imageHost.paste.scan.ready == nil })
	doc := state.session.Current()
	if doc.Text() != "新甲乙丙\r\n" {
		t.Fatalf("async paste changed the draft: %q", doc.Text())
	}
	clickID(t, driver, imagePasteActionID)
	if doc.Text() != "新![image](<https://cdn.example/a.png>)甲乙丙\r\n" {
		t.Fatalf("did not insert at current caret: %q", doc.Text())
	}
}

func TestImageHostSettingsSaveTestAndMask(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "en"
	store := imagehost.ConfigStore{Path: filepath.Join(t.TempDir(), "private", "imagehost.json")}
	if err := state.enableImageHost(store); err != nil {
		t.Fatal(err)
	}
	state.openSettings()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, settingsImageHostID)
	clickID(t, driver, imageHostHTTPID)
	clickID(t, driver, imageHostHTTPEndpoint)
	if err := driver.Type("https://upload.example"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, imageHostHTTPHeaders)
	if err := driver.Type(`{"Authorization":"Bearer private-value"}`); err != nil {
		t.Fatal(err)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == imageHostHTTPHeaders && (!hit.Secure || hit.Value != "") {
			t.Fatal("secret exposed to accessibility")
		}
	}
	if hasText(driver, "private-value") || driver.Snapshot().Editor == nil || !driver.Snapshot().Editor.Secure {
		t.Fatal("secret input is not masked")
	}
	state.imageHost.test = func(_ context.Context, cfg imagehost.Config) error {
		if cfg.HTTP.Endpoint == "https://upload.example" && cfg.HTTP.Headers["Authorization"] == "Bearer private-value" {
			return nil
		}
		return errors.New("wrong test configuration")
	}
	clickID(t, driver, imageHostTestID)
	waitImageHost(t, driver, func() bool { return hasText(driver, "Test upload succeeded") })
	if cfg, err := store.Load(); err != nil || cfg.Mode != imagehost.ModeLocal {
		t.Fatal("test upload saved settings")
	}
	clickID(t, driver, imageHostSaveID)
	if cfg, err := store.Load(); err != nil || cfg.Mode != imagehost.ModeHTTP || cfg.HTTP.Headers["Authorization"] != "Bearer private-value" || state.imageHost.cfg.Mode != cfg.Mode {
		t.Fatal("settings did not save to private image host store")
	}
}

func imageHostPasteFixture(t *testing.T) (*model, *app.Driver, string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("\ufeff甲乙丙\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.preferences.Language = "en"
	state.imageHost.cfg = imagehost.Config{Mode: imagehost.ModeHTTP, HTTP: imagehost.HTTPConfig{Endpoint: "https://upload.example"}}
	if !state.openFile(path) {
		t.Fatal(state.message)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	if err := driver.SelectText(0, 0); err != nil {
		t.Fatal(err)
	}
	bitmap := imagePNG(t, 2, 1)
	if err := driver.SetClipboardImage(bitmap); err != nil {
		t.Fatal(err)
	}
	return state, driver, path, bitmap
}

func pasteImageHostClipboard(t *testing.T, driver *app.Driver) {
	t.Helper()
	if handled, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil || !handled {
		t.Fatalf("clipboard image paste handled=%v err=%v", handled, err)
	}
}

func waitImageHost(t *testing.T, driver *app.Driver, ready func() bool) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for !ready() {
		select {
		case <-driver.Wakes():
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for image host result")
		}
	}
}
