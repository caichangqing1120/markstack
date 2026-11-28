package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
)

func TestSettingsPersistAppearanceAndSeparateSourceReadingSizes(t *testing.T) {
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	state, driver := testDriver(t)
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, settingsWritingID)
	clickID(t, driver, sourceSizeIncreaseID)
	clickID(t, driver, sourceSizeIncreaseID)
	clickID(t, driver, readingSizeIncreaseID)
	clickID(t, driver, readingSoftID)
	clickID(t, driver, settingsGeneralID)
	clickID(t, driver, appearanceDarkID)
	if state.preferences.SourceSize != 20 || state.preferences.ReadingSize != 18 || state.preferences.ReadingTheme != "soft" || !state.dark || driver.NativeAppearance() != app.AppearanceDark {
		t.Fatalf("settings not applied: %+v dark=%v native=%v", state.preferences, state.dark, driver.NativeAppearance())
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil || state.settingsOpen {
		t.Fatalf("settings Escape did not close panel: %v", err)
	}
	loaded, err := store.Load()
	if err != nil || loaded != state.preferences {
		t.Fatalf("stored settings=%+v, %v", loaded, err)
	}
	second := newModel()
	if err := second.enablePreferences(store); err != nil || !second.dark || second.preferences != loaded {
		t.Fatalf("restart settings=%+v dark=%v err=%v", second.preferences, second.dark, err)
	}
	clickID(t, driver, themeToggleID)
	if current, err := store.Load(); err != nil || current.Dark || state.dark {
		t.Fatalf("toolbar theme toggle and settings diverged: %+v, %v", current, err)
	}
}

func TestSettingsReadingThemesAreLocalizedAndPreviewImmediately(t *testing.T) {
	state, driver := testDriver(t)
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	state.session.Current().Replace("Preview body\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	for _, language := range []string{"zh-CN", "en"} {
		if language == "en" {
			clickID(t, driver, settingsGeneralID)
			clickID(t, driver, languageEnglishID)
		} else {
			clickID(t, driver, languageChineseID)
		}
		clickID(t, driver, settingsWritingID)
		for _, option := range []struct {
			id            gfx.ID
			value, zh, en string
		}{
			{readingSoftID, "soft", "柔和", "Soft"},
			{readingGitHubID, "github", "GitHub", "GitHub"},
			{readingParchmentID, "parchment", "羊皮纸", "Parchment"},
			{readingAcademicID, "academic", "典雅", "Classic"},
			{readingNordID, "nord", "Nord", "Nord"},
			{readingStandardID, "standard", "标准", "Standard"},
		} {
			clickID(t, driver, option.id)
			if persisted, err := store.Load(); err != nil || persisted.ReadingTheme != option.value {
				t.Fatalf("theme %q not persisted: %+v, %v", option.value, persisted, err)
			}
			label := state.local(option.zh, option.en)
			selected := false
			frame := driver.Snapshot()
			for _, hit := range frame.Hits {
				if hit.ID == option.id {
					selected = hit.Selected && strings.HasPrefix(hit.Label, label)
				}
			}
			_, palette := state.look()
			appearance := state.markdownAppearance(palette)
			if !state.settingsOpen || !selected {
				t.Fatalf("missing selected %s option %q", language, label)
			}
			found := false
			for _, drawn := range frame.Texts {
				if drawn.Value == "Aa" && drawn.Style.Color == appearance.palette.ink {
					found = true
				}
			}
			if !found {
				t.Fatalf("theme %s did not refresh the settings sample", option.value)
			}
		}
	}
	if state.session.Current().Text() != "Preview body\n" {
		t.Fatal("changing the theme changed the draft")
	}
}

func TestSettingsAutoSaveSwitchPersistsAndRollsBackOnWriteFailure(t *testing.T) {
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	state, driver := testDriver(t)
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, settingsWritingID)
	if state.preferences.AutoSave {
		t.Fatal("auto-save was enabled by default")
	}
	clickID(t, driver, autoSaveToggleID)
	if !state.preferences.AutoSave {
		t.Fatal("auto-save switch did not enable")
	}
	if stored, err := store.Load(); err != nil || !stored.AutoSave {
		t.Fatalf("enabled setting not persisted: %+v, %v", stored, err)
	}
	restarted := newModel()
	if err := restarted.enablePreferences(store); err != nil || !restarted.preferences.AutoSave {
		t.Fatalf("restart lost auto-save setting: %+v, %v", restarted.preferences, err)
	}
	clickID(t, driver, autoSaveToggleID)
	if state.preferences.AutoSave {
		t.Fatal("auto-save switch did not disable")
	}
	unsafe := filepath.Join(t.TempDir(), "loose", "settings.json")
	if err := os.MkdirAll(filepath.Dir(unsafe), 0755); err != nil {
		t.Fatal(err)
	}
	state.settingsStore = &session.PreferencesStore{Path: unsafe}
	clickID(t, driver, autoSaveToggleID)
	if state.preferences.AutoSave || state.settingsError == "" {
		t.Fatalf("failed preference write enabled auto-save: %+v error=%q", state.preferences, state.settingsError)
	}
}

func TestSettingsSuccessfulRetryClearsOnlyItsOwnWarning(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		toolbarFirst  bool
		toolbarRetry  bool
		reopen        bool
		documentAlert bool
		recoveryAlert bool
	}{
		{name: "panel"},
		{name: "reopen", reopen: true},
		{name: "toolbar", toolbarFirst: true, toolbarRetry: true},
		{name: "toolbar-to-panel", toolbarFirst: true},
		{name: "panel-to-toolbar", toolbarRetry: true},
		{name: "document-warning", documentAlert: true},
		{name: "recovery-warning", recoveryAlert: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "settings")
			store := session.PreferencesStore{Path: filepath.Join(directory, "settings.json")}
			original := session.DefaultPreferences()
			original.Dark = true
			if err := store.Save(original); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(store.Path)
			if err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			if err := state.enablePreferences(store); err != nil {
				t.Fatal(err)
			}
			openPanel := func() {
				clickID(t, driver, fileMenuID)
				clickID(t, driver, menuSettingsID)
			}
			closePanel := func() {
				if err := driver.Key(gfx.KeyEscape, 0); err != nil || state.settingsOpen {
					t.Fatalf("settings Escape did not close panel: %v", err)
				}
			}
			if !scenario.toolbarFirst {
				openPanel()
			}
			if err := os.Chmod(directory, 0755); err != nil {
				t.Fatal(err)
			}
			if scenario.toolbarFirst {
				clickID(t, driver, themeToggleID)
			} else {
				clickID(t, driver, appearanceLightID)
			}
			const settingsWarning = "设置文件路径或权限不安全，未更改"
			if !hasText(driver, settingsWarning) || !state.dark {
				t.Fatal("denied settings write did not retain the theme and show its warning")
			}
			if after, err := os.ReadFile(store.Path); err != nil || !bytes.Equal(after, before) {
				t.Fatalf("denied settings write changed the stored bytes: %q, %v", after, err)
			}
			if err := os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if scenario.reopen || scenario.toolbarRetry && !scenario.toolbarFirst {
				closePanel()
			}
			if scenario.reopen || !scenario.toolbarRetry && scenario.toolbarFirst {
				openPanel()
				if state.settingsError != settingsWarning {
					t.Error("opening settings hid the unresolved write warning from the panel")
				}
			}
			const documentWarning = "document changed on disk"
			if scenario.documentAlert {
				state.message = documentWarning
			}
			if scenario.recoveryAlert {
				state.recoveryWriteErr = errors.New("journal unavailable")
			}
			if scenario.toolbarRetry {
				clickID(t, driver, themeToggleID)
			} else {
				clickID(t, driver, appearanceLightID)
			}
			expected := original
			expected.Dark = false
			if stored, err := store.Load(); err != nil || stored != expected || state.dark {
				t.Fatalf("retry did not persist and apply the theme: %+v, dark=%v, err=%v", stored, state.dark, err)
			}
			if state.settingsError != "" {
				t.Errorf("settings panel kept a warning after successful persistence: %q", state.settingsError)
			}
			if state.settingsOpen {
				closePanel()
			}
			if hasText(driver, settingsWarning) {
				t.Error("settings still reported a failed write after successful persistence")
			}
			if scenario.documentAlert && !hasText(driver, documentWarning) {
				t.Error("settings retry hid the document warning")
			}
			if scenario.recoveryAlert && !hasText(driver, "恢复快照失败：journal unavailable") {
				t.Error("settings retry hid the active recovery warning")
			}
		})
	}
}

func TestSettingsChangeSourceSizeWithoutTouchingCaretOrBOMCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 标题\r\n\r\n正文\r\n")...)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	current := state.session.Current()
	current.Editor.MoveTo(3, false)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, settingsWritingID)
	clickID(t, driver, sourceSizeIncreaseID)
	if current.Editor.Caret() != 3 || current.Dirty() {
		t.Fatalf("source size moved caret or changed document: caret=%d dirty=%v", current.Editor.Caret(), current.Dirty())
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, drawn := range driver.Snapshot().Texts {
		if drawn.Style.Size == 18 && drawn.Value != "" && drawn.Bounds.Y > state.sourceRect.Y {
			visible = true
		}
	}
	if !visible {
		t.Fatal("source editor did not render at 18px")
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("font setting changed Markdown bytes: %q, %v", data, err)
	}
}

func TestReadingSizeAndSoftThemeDoNotChangeRenderedEditGeometry(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Heading\n\nBody text")
	state.preferences.ReadingSize = 22
	state.preferences.ReadingTheme = "soft"
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	readingSize := float32(0)
	for _, drawn := range driver.Snapshot().Texts {
		if drawn.Value == "Body text" {
			readingSize = drawn.Style.Size
		}
	}
	if readingSize != 22 {
		t.Fatalf("reading body size=%v", readingSize)
	}
	_, base := state.look()
	soft := false
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Bounds == state.previewRect && primitive.Color != base.background {
			soft = true
		}
	}
	if state.previewRect.Width == 0 || !soft {
		t.Fatal("soft theme did not affect reading surface")
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	snapshot := driver.Snapshot()
	for i, hit := range snapshot.Hits {
		if hit.ID == richInputID {
			text, _ := snapshot.HitTextAt(i)
			if text.Size != 16 {
				t.Fatalf("Rendered editing input resized without caret migration: %+v", hit)
			}
			return
		}
	}
	t.Fatal("Rendered editing input missing")
}

func TestSettingsWriteFailureRollsBackAppearance(t *testing.T) {
	root := t.TempDir()
	loose := filepath.Join(root, "loose")
	if err := os.Mkdir(loose, 0755); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.settingsStore = &session.PreferencesStore{Path: filepath.Join(loose, "settings.json")}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	initialAppearance := driver.NativeAppearance()
	clickID(t, driver, themeToggleID)
	if state.dark || state.preferences.Dark || state.message == "" || driver.NativeAppearance() != initialAppearance {
		t.Fatalf("failed setting changed appearance: dark=%v prefs=%+v message=%q", state.dark, state.preferences, state.message)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, settingsWritingID)
	clickID(t, driver, readingSizeIncreaseID)
	if state.preferences.ReadingSize != 16 || !strings.Contains(state.settingsError, "设置文件") || !hasText(driver, state.settingsError) {
		t.Fatalf("failed panel setting was not rolled back: %+v error=%q", state.preferences, state.settingsError)
	}
}

func TestSettingsModalMinimumWidthAndStepperBounds(t *testing.T) {
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	if err := session.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, session.Driver(), fileMenuID)
	clickID(t, session.Driver(), menuSettingsID)
	if !state.settingsOpen {
		t.Fatal("settings modal not opened")
	}
	clickID(t, session.Driver(), settingsWritingID)
	for _, dark := range []bool{false, true} {
		state.dark = dark
		if err := session.Resize(740, 520); err != nil {
			t.Fatal(err)
		}
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				t.Fatalf("dark=%v settings layout=%+v", dark, problem)
			}
		}
		for _, hit := range session.Snapshot().Hits {
			if hit.ID == settingsCloseID || hit.ID == sourceSizeDecreaseID || hit.ID == sourceSizeIncreaseID || hit.ID == readingSizeDecreaseID || hit.ID == readingSizeIncreaseID || hit.ID == autoSaveToggleID {
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 740 || hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > 520 {
					t.Fatalf("settings control outside window: %+v", hit)
				}
			}
		}
	}
	for range 3 {
		clickID(t, session.Driver(), sourceSizeIncreaseID)
	}
	if state.preferences.SourceSize != 22 {
		t.Fatalf("source size did not reach 22: %+v", state.preferences)
	}
	for _, hit := range session.Snapshot().Hits {
		if hit.ID == sourceSizeIncreaseID && !hit.Disabled {
			t.Fatal("upper source-size bound is still enabled")
		}
	}
	if _, err := session.Driver().ShortcutChord(primaryModifier(), 'n'); err != nil || len(state.session.Tabs) != 1 {
		t.Fatal("settings modal leaked New shortcut into document")
	}
	if err := session.Driver().Key(gfx.KeyEscape, 0); err != nil || state.settingsOpen {
		t.Fatalf("settings modal Escape=%v open=%v", err, state.settingsOpen)
	}
}

func scrollSettings(t *testing.T, driver *app.Driver, delta float32) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == settingsBackdropID {
			if err := driver.Scroll(gfx.ScrollEvent{X: hit.Bounds.X + hit.Bounds.Width/2 + 90, Y: hit.Bounds.Y + hit.Bounds.Height/2, DeltaY: delta, Precise: true}); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("settings backdrop missing")
}

func TestSettingsSectionsScrollWithoutOverlappingControls(t *testing.T) {
	for _, size := range []struct{ width, height int }{{740, 500}, {1200, 800}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/dark=%t", size.width, size.height, dark), func(t *testing.T) {
				state, driver := testDriver(t)
				state.dark, state.preferences.Dark = dark, dark
				state.preferences.Language, state.preferences.UITextSize = "en", "large"
				if err := driver.Resize(size.width, size.height); err != nil {
					t.Fatal(err)
				}
				state.openSettings()
				if err := driver.Pump(); err != nil {
					t.Fatal(err)
				}
				check := func(visible, hidden gfx.ID) {
					t.Helper()
					frame := driver.Snapshot()
					var hits []gfx.Hit
					found := false
					for _, hit := range frame.Hits {
						if hit.Scope != settingsScope || hit.Backdrop || hit.IsLabel || hit.Presentational || hit.Block {
							continue
						}
						if hit.ID == hidden {
							t.Fatalf("unexpected visible control: %s", hit.Label)
						}
						found = found || hit.ID == visible
						for _, other := range hits {
							left, right := hit.Bounds, other.Bounds
							if left.X < right.X+right.Width && right.X < left.X+left.Width && left.Y < right.Y+right.Height && right.Y < left.Y+left.Height {
								t.Fatalf("overlapping controls: %s and %s", hit.Label, other.Label)
							}
						}
						hits = append(hits, hit)
					}
					if !found {
						t.Fatalf("missing control %v", visible)
					}
				}
				check(appearanceLightID, sourceSizeIncreaseID)
				clickID(t, driver, settingsWritingID)
				check(sourceSizeIncreaseID, appearanceLightID)
				if err := driver.Key(gfx.KeyPageDown, 0); err != nil {
					t.Fatal(err)
				}
				check(pdfAcademicID, sourceSizeIncreaseID)
				if err := driver.Key(gfx.KeyPageUp, 0); err != nil {
					t.Fatal(err)
				}
				check(sourceSizeIncreaseID, pdfAcademicID)
				scrollSettings(t, driver, 1000)
				check(pdfAcademicID, sourceSizeIncreaseID)
				clickID(t, driver, pdfAcademicID)
				if state.preferences.PDFPreset != "academic" {
					t.Fatal("scrolled PDF control did not apply")
				}
				clickID(t, driver, settingsImageHostID)
				clickID(t, driver, imageHostS3ID)
				check(imageHostS3Endpoint, pdfAcademicID)
				bar := state.settings.scrollbar
				thumbX := bar.VerticalThumb.X + bar.VerticalThumb.Width/2
				thumbY := bar.VerticalThumb.Y + bar.VerticalThumb.Height/2
				bottom := bar.VerticalTrack.Y + bar.VerticalTrack.Height
				if bar.MaxOffsetY <= 0 {
					t.Fatal("expected the S3 form to scroll")
				}
				if err := driver.Press(thumbX, thumbY, gfx.PointerButtonPrimary); err != nil {
					t.Fatal(err)
				}
				if err := driver.Move(thumbX, bottom); err != nil {
					t.Fatal(err)
				}
				if err := driver.Release(thumbX, bottom, gfx.PointerButtonPrimary); err != nil {
					t.Fatal(err)
				}
				if state.settings.scroll != bar.MaxOffsetY {
					t.Fatal("dragging the S3 scrollbar did not reach the end")
				}
				check(imageHostSaveID, sourceSizeIncreaseID)
				clickID(t, driver, settingsAboutID)
				check(copyDiagnosticsID, imageHostSaveID)
				scrollSettings(t, driver, 1000)
				check(projectIssuesID, imageHostSaveID)
				clickID(t, driver, settingsCloseID)
				if state.settingsOpen {
					t.Fatal("fixed close button stopped working after scroll")
				}
			})
		}
	}
}

func TestRichHTMLReadingSizeDoesNotAlterSourceBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.mh")
	source := "<p>富文本文字</p>\r\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.preferences.ReadingSize = 22
	state.preferences.ReadingTheme = "soft"
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	for _, drawn := range driver.Snapshot().Texts {
		if drawn.Value == "富文本文字" && drawn.Style.Size == 22 {
			if err := state.session.Save(state.session.Active, ""); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != source {
				t.Fatalf("reading settings changed HTML bytes: %q, %v", data, err)
			}
			return
		}
	}
	t.Fatal("rich reading did not render at 22px")
}

func TestSourceSizeAppliesToCodePlainAndRichSource(t *testing.T) {
	for _, extension := range []string{"go", "txt", "mh"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note."+extension)
			if err := os.WriteFile(path, []byte("source text"), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.preferences.SourceSize = 20
			state.openFile(path)
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			if extension == "mh" {
				clickID(t, driver, editModeID)
			}
			for _, drawn := range driver.Snapshot().Texts {
				if drawn.Style.Size == 20 && drawn.Bounds.Y >= state.sourceRect.Y && drawn.Bounds.Y < state.sourceRect.Y+state.sourceRect.Height {
					if state.session.Current().Dirty() {
						t.Fatal("source size changed document")
					}
					return
				}
			}
			t.Fatal("source editor did not use 20px size")
		})
	}
}

// TestSettingsRefuseCorruptAndUnsafeConfigurationWithoutOverwriting covers the
// panel path for a corrupt, symlink-swapped and wide-permission settings file:
// loading reports the failure, every change is rolled back with a specific
// message, and the existing bytes are never overwritten.
func TestSettingsRefuseCorruptAndUnsafeConfigurationWithoutOverwriting(t *testing.T) {
	private := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	assertRejected := func(t *testing.T, state *model, driver *app.Driver, store session.PreferencesStore, wantError, wantMessage string) {
		t.Helper()
		initialAppearance := driver.NativeAppearance()
		if err := state.enablePreferences(store); err == nil || !strings.Contains(err.Error(), wantError) {
			t.Fatalf("load error=%v want %q", err, wantError)
		}
		if state.preferences != session.DefaultPreferences() || state.dark {
			t.Fatalf("rejected settings changed state: %+v dark=%v", state.preferences, state.dark)
		}
		if err := driver.Resize(1200, 780); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, fileMenuID)
		clickID(t, driver, menuSettingsID)
		clickID(t, driver, appearanceDarkID)
		if state.dark || state.preferences.Dark || driver.NativeAppearance() != initialAppearance {
			t.Fatalf("rejected write changed appearance: dark=%v prefs=%+v native=%v", state.dark, state.preferences, driver.NativeAppearance())
		}
		if !strings.Contains(state.settingsError, wantMessage) || !hasText(driver, state.settingsError) {
			t.Fatalf("settings error=%q want %q", state.settingsError, wantMessage)
		}
		if err := driver.Key(gfx.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
	}
	corrupt := []byte(`{"version":1,"sourceSize":16,"readingSize":16,"readingTheme":"standard","unexpected":true}`)
	corruptPath := filepath.Join(private, "settings.json")
	if err := os.WriteFile(corruptPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	assertRejected(t, state, driver, session.PreferencesStore{Path: corruptPath}, "corrupt", "设置文件已损坏")
	if data, err := os.ReadFile(corruptPath); err != nil || !bytes.Equal(data, corrupt) {
		t.Fatalf("corrupt settings overwritten: %q, %v", data, err)
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	outsideBytes := []byte(`{"version":1,"dark":true,"sourceSize":16,"readingSize":16,"readingTheme":"standard"}`)
	if err := os.WriteFile(outside, outsideBytes, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(private, "linked.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	linked, linkedDriver := testDriver(t)
	assertRejected(t, linked, linkedDriver, session.PreferencesStore{Path: link}, "unsafe", "设置文件路径或权限不安全")
	if data, err := os.ReadFile(outside); err != nil || !bytes.Equal(data, outsideBytes) {
		t.Fatalf("symlinked settings target changed: %q, %v", data, err)
	}

	wide := filepath.Join(private, "wide.json")
	wideBytes := []byte(`{"version":1,"dark":false,"sourceSize":16,"readingSize":16,"readingTheme":"standard"}`)
	if err := os.WriteFile(wide, wideBytes, 0644); err != nil {
		t.Fatal(err)
	}
	looseFile, looseDriver := testDriver(t)
	assertRejected(t, looseFile, looseDriver, session.PreferencesStore{Path: wide}, "unsafe", "设置文件路径或权限不安全")
	if data, err := os.ReadFile(wide); err != nil || !bytes.Equal(data, wideBytes) {
		t.Fatalf("wide-permission settings overwritten: %q, %v", data, err)
	}
}

// TestSettingsStayIndependentOfReadingAndRenderedTypographicScale checks the
// 14-22px bounds of both steppers, that the two sizes apply only to their own
// view modes at 1200 and 740 pixels, and that changing either one leaves the
// document bytes, dirty state, selection and caret untouched.
func TestSettingsStayIndependentOfReadingAndRenderedTypographicScale(t *testing.T) {
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	state, driver := testDriver(t)
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("# 标题\n\n正文 text\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state.openFile(path)
	current := state.session.Current()
	current.Editor.SetSelection(text.Range{Start: 1, End: 4})
	wantSelection, wantCaret, wantBytes := current.Editor.Selection(), current.Editor.Caret(), current.Text()
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, menuSettingsID)
	clickID(t, driver, settingsWritingID)
	for range 3 {
		clickID(t, driver, sourceSizeIncreaseID)
	}
	if state.preferences.SourceSize != 22 {
		t.Fatalf("source size=%d want 22", state.preferences.SourceSize)
	}
	stepperDisabled := func(id gfx.ID) bool {
		t.Helper()
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				return hit.Disabled
			}
		}
		t.Fatalf("stepper %v missing", id)
		return false
	}
	if !stepperDisabled(sourceSizeIncreaseID) || stepperDisabled(sourceSizeDecreaseID) {
		t.Fatal("source-size upper bound does not disable only the increase")
	}
	for range 4 {
		clickID(t, driver, sourceSizeDecreaseID)
	}
	if state.preferences.SourceSize != 14 || !stepperDisabled(sourceSizeDecreaseID) {
		t.Fatalf("source size lower bound=%d", state.preferences.SourceSize)
	}
	for range 4 {
		clickID(t, driver, sourceSizeIncreaseID)
	}
	clickID(t, driver, readingSizeDecreaseID)
	if state.preferences.ReadingSize != 14 || !stepperDisabled(readingSizeDecreaseID) || stepperDisabled(readingSizeIncreaseID) {
		t.Fatalf("reading size lower bound=%+v", state.preferences)
	}
	clickID(t, driver, readingSizeIncreaseID)
	clickID(t, driver, readingSizeIncreaseID)
	clickID(t, driver, readingSizeIncreaseID)
	if state.preferences.ReadingSize != 20 {
		t.Fatalf("reading size=%d want 20", state.preferences.ReadingSize)
	}
	clickID(t, driver, readingSizeDecreaseID)
	clickID(t, driver, readingSizeDecreaseID)
	clickID(t, driver, readingSizeDecreaseID)
	if state.preferences.ReadingSize != 14 || !stepperDisabled(readingSizeDecreaseID) {
		t.Fatalf("reading size did not return to its lower bound: %+v", state.preferences)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if current.Editor.Selection() != wantSelection || current.Editor.Caret() != wantCaret || current.Text() != wantBytes || current.Dirty() {
		t.Fatalf("settings changed the document: selection=%+v caret=%d dirty=%v", current.Editor.Selection(), current.Editor.Caret(), current.Dirty())
	}
	clickID(t, driver, splitModeID)
	sizesIn := func(rect ui.Rect) map[float32]bool {
		sizes := map[float32]bool{}
		for _, label := range driver.Snapshot().Texts {
			if label.Value == "" || !label.HasClip || label.ClipOf != rect {
				continue
			}
			sizes[label.Style.Size] = true
		}
		return sizes
	}
	sourceSized := func() bool {
		for _, label := range driver.Snapshot().Texts {
			if label.Value != "" && label.Style.Size == 22 && label.Bounds.Y >= state.sourceRect.Y && label.Bounds.Y < state.sourceRect.Y+state.sourceRect.Height {
				return true
			}
		}
		return false
	}
	if !sourceSized() {
		t.Errorf("source pane did not render at 22px")
	}
	if sizes := sizesIn(state.previewRect); !sizes[14] {
		t.Errorf("reading pane sizes=%v want 14px", sizes)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if !sourceSized() {
		t.Errorf("narrow source pane did not render at 22px")
	}
	if sizes := sizesIn(state.previewRect); !sizes[14] {
		t.Errorf("narrow reading pane sizes=%v want 14px", sizes)
	}
	expected := session.DefaultPreferences()
	expected.SourceSize, expected.ReadingSize = 22, 14
	if state.preferences != expected {
		t.Fatalf("resize changed settings: %+v", state.preferences)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	rendered := false
	snapshot := driver.Snapshot()
	for i, hit := range snapshot.Hits {
		if hit.ID == richInputID {
			rendered = true
			text, _ := snapshot.HitTextAt(i)
			if text.Size != 16 {
				t.Fatalf("Rendered input adopted a reading/source scale: %+v", hit)
			}
		}
	}
	if !rendered {
		t.Fatal("Rendered editing input missing")
	}
	if current.Text() != wantBytes || current.Dirty() {
		t.Fatalf("mode switch changed document bytes: dirty=%v", current.Dirty())
	}
}
