package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/update"
	"markstack/internal/session"
	"markstack/internal/updater"
)

func TestM6DriverSettingsAndAbout(t *testing.T) {
	state, driver := testDriver(t)
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	state.newDocument()
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, languageEnglishID)
	if !hasText(driver, "Appearance & General") {
		t.Fatal("language not applied to settings")
	}
	clickID(t, driver, textLargeID)
	clickID(t, driver, tabNumbersID)
	clickID(t, driver, configDirectoryID)
	if opened, ok := driver.LastRevealedPath(); !ok || opened != store.Path {
		t.Fatalf("config directory: %q, %t", opened, ok)
	}
	clickID(t, driver, settingsWritingID)
	clickID(t, driver, sourceSizeIncreaseID)
	clickID(t, driver, settingsAboutID)
	clickID(t, driver, autoCheckUpdatesID)
	clickID(t, driver, copyDiagnosticsID)
	if !hasText(driver, "Copied") {
		t.Fatal("clipboard confirmation missing")
	}
	if diagnostics := driver.Clipboard(); !strings.Contains(diagnostics, "Platform:") || !strings.Contains(diagnostics, "Viva:") || strings.Contains(diagnostics, store.Path) {
		t.Fatalf("diagnostics copy = %q", diagnostics)
	}
	clickID(t, driver, projectIssuesID)
	if opened, ok := driver.LastOpenedURL(); !ok || opened != projectURL+"/issues" {
		t.Fatalf("issues link = %q, %t", opened, ok)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if !hasText(driver, "1. "+state.session.Tabs[0].Title) {
		t.Fatal("tab number not shown")
	}
	if handled, err := driver.ShortcutChord(primaryModifier(), '1'); err != nil || !handled || state.session.Active != 0 {
		t.Fatalf("tab shortcut: handled=%t active=%d err=%v", handled, state.session.Active, err)
	}
	stored, err := store.Load()
	if err != nil || stored.Language != "en" || stored.UITextSize != "large" || !stored.ShowTabNumbers || !stored.AutoCheckUpdates || stored.SourceSize != 18 || stored.ReadingSize != 16 {
		t.Fatalf("persisted settings %+v, %v", stored, err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, resetPreferencesID)
	if err := driver.Key(gfx.KeyEscape, 0); err != nil || !state.settingsOpen || state.preferences != stored {
		t.Fatal("reset confirmation did not cancel safely")
	}
	clickID(t, driver, resetPreferencesID)
	clickID(t, driver, confirmResetPreferencesID)
	if defaults, err := store.Load(); err != nil || defaults != session.DefaultPreferences() {
		t.Fatalf("reset failed: %+v, %v", defaults, err)
	}
}

func TestPDFSettingsPersistAndApplyAtExport(t *testing.T) {
	requirePDFFont(t)
	state, driver := testDriver(t)
	root := t.TempDir()
	store := session.PreferencesStore{Path: filepath.Join(root, "private", "settings.json")}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 500); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, languageEnglishID)
	clickID(t, driver, settingsWritingID)
	if !hasText(driver, "PDF Export") || !hasText(driver, "Running header") || !hasText(driver, "Page numbers") || !hasText(driver, "Layout preset") {
		t.Fatal("English PDF settings labels are missing")
	}
	clickID(t, driver, pdfHeaderID)
	clickID(t, driver, pdfFooterID)
	clickID(t, driver, pdfCompactID)
	if stored, err := store.Load(); err != nil || !stored.PDFHeader || !stored.PDFFooter || stored.PDFPreset != "compact" {
		t.Fatalf("PDF settings were not saved: %+v, %v", stored, err)
	}
	clickID(t, driver, settingsCloseID)
	state.newDocument()
	current := state.session.Current()
	current.Replace("# Body\n\n" + strings.Repeat("One line of body text.\n\n", 62))
	target := filepath.Join(root, "export.pdf")
	state.exportPDFWithPicker(func(app.SaveFileOptions) (string, error) { return target, nil })
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("PDF was not exported: %v (%s)", err, state.message)
	}
	pages := inspectExportedPDF(t, data).pageTexts
	if len(pages) != 2 || !slices.Contains(pages[0], "1 / 2") || !slices.Contains(pages[1], "2 / 2") || !slices.Contains(pages[1], current.Title) {
		t.Fatalf("PDF settings not applied: %q", pages)
	}
}

type driverUpdateService struct {
	checked       chan struct{}
	started       chan struct{}
	waitForCancel bool
	installErr    error
	installed     int
}

func (service *driverUpdateService) Check(context.Context) (*updater.Offer, error) {
	if service.checked != nil {
		service.checked <- struct{}{}
	}
	return &updater.Offer{Version: "0.2.0", PatchSize: 1468006, FullSize: 20237517}, nil
}

func TestM6DriverStartupCheckIsOptIn(t *testing.T) {
	state, _ := testDriver(t)
	service := &driverUpdateService{checked: make(chan struct{}, 1)}
	state.updates.service = service
	state.startUpdateCheckOnLaunch()
	if state.updates.busy() {
		t.Fatal("startup check was scheduled without opt-in")
	}
	state.preferences.AutoCheckUpdates = true
	state.startUpdateCheckOnLaunch()
	select {
	case <-service.checked:
	case <-time.After(3 * time.Second):
		t.Fatal("startup check was not scheduled after opt-in")
	}
}

func (service *driverUpdateService) Prepare(ctx context.Context, _ *updater.Offer, progress func(updater.Progress)) (*updater.Prepared, error) {
	progress(updater.Progress{Done: 50, Total: 100, Delta: true})
	if service.waitForCancel {
		close(service.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &updater.Prepared{}, nil
}

func (service *driverUpdateService) Install(_ *updater.Prepared, quit func()) error {
	service.installed++
	if service.installErr != nil {
		return service.installErr
	}
	quit()
	return nil
}

func TestM6DriverUpdateFlowAndQuitGate(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "en"
	service := &driverUpdateService{started: make(chan struct{}), waitForCancel: true, installErr: fmt.Errorf("dpkg: %w", update.ErrManagedExternally)}
	state.updates.service = service
	quit := false
	state.updates.quit = func() { quit = true }
	advanceUntil := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !condition() && time.Now().Before(deadline) {
			if err := driver.Advance(time.Millisecond); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
		if !condition() {
			t.Fatal("update UI did not reach expected state")
		}
	}
	draft := state.session.Current()
	draft.Editor.Insert("unsaved draft")
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuCheckUpdatesID)
	if !hasText(driver, "About & Updates") {
		t.Fatal("file menu did not open About")
	}
	clickID(t, driver, checkUpdatesID)
	advanceUntil(func() bool { return state.updates.offer != nil })
	if !hasText(driver, "Delta 1.4 MiB / Full 19.3 MiB") {
		t.Fatal("update size badge missing")
	}
	clickID(t, driver, downloadUpdateID)
	select {
	case <-service.started:
	case <-time.After(3 * time.Second):
		t.Fatal("download not started")
	}
	advanceUntil(func() bool { return hasText(driver, "50%") })
	clickID(t, driver, cancelUpdateID)
	advanceUntil(func() bool { return !state.updates.busy() })
	if !hasText(driver, "Update canceled") {
		t.Fatal("cancel feedback missing")
	}
	service.waitForCancel = false
	clickID(t, driver, downloadUpdateID)
	advanceUntil(func() bool { return state.updates.prepared != nil })
	clickID(t, driver, installUpdateID)
	if state.session.Pending() == nil || service.installed != 0 {
		t.Fatal("installed before resolving dirty draft")
	}
	clickID(t, driver, cancelConfirmID)
	if !state.settingsOpen || service.installed != 0 || quit || !draft.Dirty() {
		t.Fatal("cancel lost draft or started install")
	}
	clickID(t, driver, installUpdateID)
	clickID(t, driver, discardConfirmID)
	advanceUntil(func() bool { return !state.updates.installing })
	if service.installed != 1 || quit || state.recoveryDiscarded || !draft.Dirty() || !strings.Contains(state.updateStatusText(), "package manager") {
		t.Fatalf("managed install: %q, draft=%v quit=%v", state.updateStatusText(), draft.Dirty(), quit)
	}
	service.installErr = nil
	clickID(t, driver, installUpdateID)
	clickID(t, driver, discardConfirmID)
	advanceUntil(func() bool { return quit })
	if service.installed != 2 || !state.recoveryDiscarded {
		t.Fatal("approved install did not finish shutdown")
	}
}
