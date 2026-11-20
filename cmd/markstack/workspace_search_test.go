package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

func TestRichHTMLWorkspaceResultOpensVisibleMatchAndRejectsStaleDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rich.mh")
	original := []byte(`<p>A &amp; 目标</p><h2>标题</h2>`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "A & 目标"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 1 {
		t.Fatalf("rich hits=%+v, %v", hits, err)
	}
	state.workspaceSearch.open = true
	state.workspaceSearch.results = hits
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == workspaceResultRows.At(0) && hit.Disabled {
			t.Fatal("rich search result is disabled")
		}
	}
	clickID(t, driver, workspaceResultRows.At(0))
	if state.session.Current().Kind != workspace.RichHTMLKind || state.mode != renderedMode || state.html.workspaceMatch == nil || state.html.workspaceMatch.block != 0 || state.session.Current().Dirty() {
		t.Fatalf("rich result did not navigate visually: mode=%v match=%+v", state.mode, state.html.workspaceMatch)
	}
	if err := os.WriteFile(path, []byte(`<p>external update</p>`), 0644); err != nil {
		t.Fatal(err)
	}
	state.message = ""
	state.openWorkspaceResult(hits[0])
	if state.message == "" || state.session.Current().Text() != string(original) {
		t.Fatal("stale result overwrote an open rich tab")
	}
}

func TestRichHTMLWorkspaceResultScrollsToVisibleHighlight(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "long.mh")
	source := strings.Repeat("<p>before</p>\n", 28) + "<p>最终目标</p>\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "最终目标"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 1 {
		t.Fatalf("rich search=%+v, %v", hits, err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	state.openWorkspaceResult(hits[0])
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(741, 520); err != nil {
		t.Fatal(err)
	}
	if state.html.scroll <= 0 || state.session.Current().Text() != source {
		t.Fatalf("result did not scroll without editing: scroll=%v", state.html.scroll)
	}
	var text ui.Rect
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "最终目标" {
			text = label.Bounds
		}
	}
	if text.Width == 0 || text.Y < 64 || text.Y > 500 {
		t.Fatalf("target not visible after scroll: %+v scroll=%v height=%v viewport=%+v block=%d top=%v", text, state.html.scroll, state.html.height, state.html.rect, hits[0].RichBlock, state.html.blockY[hits[0].RichBlock])
	}
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Color.Alpha > 0.3 && primitive.Color.Alpha < 0.5 && primitive.Bounds.X < text.X+text.Width && primitive.Bounds.X+primitive.Bounds.Width > text.X && primitive.Bounds.Y < text.Y+text.Height && primitive.Bounds.Y+primitive.Bounds.Height > text.Y {
			return
		}
	}
	t.Fatal("workspace result has no visible highlight")
}

func TestRichHTMLWorkspaceResultRejectsDirtyAlias(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alias.mh")
	if err := os.WriteFile(path, []byte("<p>目标</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "目标"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 1 {
		t.Fatalf("search=%+v, %v", hits, err)
	}
	state := newModel()
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("<p>本地草稿</p>")
	state.openWorkspaceResult(hits[0])
	if state.message == "" || state.session.Current() != document || document.Text() != "<p>本地草稿</p>" || len(state.session.Tabs) != 2 {
		t.Fatalf("dirty alias changed on result navigation: message=%q tabs=%d", state.message, len(state.session.Tabs))
	}
}

func TestRichHTMLWorkspaceHighlightClearsAfterEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "editable.mh")
	if err := os.WriteFile(path, []byte("<p>目标</p>\n<!--keep-->"), 0644); err != nil {
		t.Fatal(err)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "目标"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 1 {
		t.Fatalf("search=%+v, %v", hits, err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	state.openWorkspaceResult(hits[0])
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if state.html.workspaceMatch != nil || state.session.Current().Text() != "<p>目标新</p>\n<!--keep-->" {
		t.Fatalf("stale highlight survived edit: match=%+v text=%q", state.html.workspaceMatch, state.session.Current().Text())
	}
}

func TestWorkspaceSearchDropsLatePreviousGenerationHits(t *testing.T) {
	state := newModel()
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var workers sync.WaitGroup
	state.workspaceSearch.worker = func(ctx context.Context, root string, options workspace.SearchOptions, emit func(workspace.SearchHit) bool) (workspace.SearchSummary, error) {
		workers.Add(1)
		defer workers.Done()
		if options.Query == "old" {
			close(oldStarted)
			<-releaseOld
			emit(workspace.SearchHit{Relative: "old.md"})
			return workspace.SearchSummary{Emitted: 1}, ctx.Err()
		}
		emit(workspace.SearchHit{Relative: "new.md"})
		return workspace.SearchSummary{Emitted: 1}, nil
	}
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	state.startWorkspaceSearch(t.TempDir(), workspace.SearchOptions{Query: "old"})
	<-oldStarted
	state.startWorkspaceSearch(t.TempDir(), workspace.SearchOptions{Query: "new"})
	close(releaseOld)
	deadline := time.After(3 * time.Second)
	for state.workspaceSearch.searching {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("new search did not complete")
		default:
			runtime.Gosched()
		}
	}
	workers.Wait()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if len(state.workspaceSearch.results) != 1 || state.workspaceSearch.results[0].Relative != "new.md" || state.workspaceSearch.error != "" {
		t.Fatalf("stale results=%+v error=%q", state.workspaceSearch.results, state.workspaceSearch.error)
	}
}

func TestWorkspaceSearchCancelStopsWorkerAndIgnoresItsCompletion(t *testing.T) {
	state := newModel()
	started := make(chan struct{})
	finished := make(chan struct{})
	reason := make(chan error, 1)
	state.workspaceSearch.worker = func(ctx context.Context, _ string, _ workspace.SearchOptions, emit func(workspace.SearchHit) bool) (workspace.SearchSummary, error) {
		close(started)
		<-ctx.Done()
		defer close(finished)
		reason <- ctx.Err()
		emit(workspace.SearchHit{Relative: "stale.md"})
		return workspace.SearchSummary{}, ctx.Err()
	}
	state.startWorkspaceSearch(t.TempDir(), workspace.SearchOptions{Query: "cancel"})
	<-started
	state.cancelWorkspaceSearch()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled worker did not exit")
	}
	state.drainWorkspaceSearch()
	if state.workspaceSearch.searching || len(state.workspaceSearch.results) != 0 || !errors.Is(<-reason, context.Canceled) {
		t.Fatalf("canceled search state=%+v", state.workspaceSearch)
	}
}

func TestWorkspaceSearchCancelsWhenWorkspaceChanges(t *testing.T) {
	oldRoot, newRoot := t.TempDir(), t.TempDir()
	state, driver := testDriver(t)
	started := make(chan struct{})
	finished := make(chan struct{})
	state.workspaceSearch.worker = func(ctx context.Context, _ string, _ workspace.SearchOptions, emit func(workspace.SearchHit) bool) (workspace.SearchSummary, error) {
		close(started)
		<-ctx.Done()
		defer close(finished)
		emit(workspace.SearchHit{Relative: "old.md"})
		return workspace.SearchSummary{}, ctx.Err()
	}
	state.startWorkspaceSearch(oldRoot, workspace.SearchOptions{Query: "old"})
	<-started
	if err := driver.QueueOpenDirectory(newRoot, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, openFolderID)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("old workspace search was not canceled")
	}
	state.drainWorkspaceSearch()
	if state.session.Root == oldRoot || state.workspaceSearch.searching || len(state.workspaceSearch.results) != 0 {
		t.Fatalf("workspace switch left stale search: root=%q state=%+v", state.session.Root, state.workspaceSearch)
	}
}

func TestWorkspaceSearchShortcutFindsAndOpensMarkdownMatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Title\n目标 内容\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("open workspace changed=%v err=%v", changed, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'f'); err != nil {
		t.Fatal(err)
	}
	if !state.workspaceSearch.open || driver.Snapshot().Focused != workspaceQueryID {
		t.Fatalf("global search not focused: %+v", state.workspaceSearch)
	}
	if err := driver.Type("目标"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	awaitWorkspaceSearch(t, driver, state)
	if len(state.workspaceSearch.results) != 1 || state.workspaceSearch.results[0].Line != 2 || !hasText(driver, "目标 内容") {
		t.Fatalf("global results=%+v", state.workspaceSearch.results)
	}
	clickID(t, driver, workspaceResultRows.At(0))
	for range 3 {
		if driver.Snapshot().Focused == sourceID {
			break
		}
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	current := state.session.Current()
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if current.Path != canonicalPath || state.mode != editMode || current.Editor.Selection().Start != 8 || current.Editor.Selection().End != 10 || driver.Snapshot().Focused != sourceID {
		t.Fatalf("search result navigation path=%q mode=%v selection=%+v focus=%v", current.Path, state.mode, current.Editor.Selection(), driver.Snapshot().Focused)
	}
}

func TestWorkspaceSearchRejectsStaleDiskHitWithoutTouchingDraft(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("target\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Current().Replace("unsaved draft")
	state.session.Root = root
	state.openWorkspaceSearch()
	state.workspaceSearch.queryEditor.Insert("target")
	state.runWorkspaceSearch()
	awaitWorkspaceSearch(t, driver, state)
	if err := os.WriteFile(path, []byte("new target moved\n"), 0644); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, workspaceResultRows.At(0))
	if state.session.Current().Text() != "unsaved draft" || state.session.Current().Path != "" || !state.session.Current().Dirty() || state.message == "" {
		t.Fatalf("stale result touched draft: text=%q path=%q message=%q", state.session.Current().Text(), state.session.Current().Path, state.message)
	}
}

func TestWorkspaceSearchChecksSavedRevisionBeforeReusingCleanTab(t *testing.T) {
	for _, fixture := range []struct{ name, body string }{
		{"cached.md", "# Cache\r\n\r\nAA OLDMATCH ZZ.\r\n"},
		{"cached.mh", "<p>AA <b>OLDMATCH</b> ZZ.</p>\r\n<!--keep-->\r\n"},
		{"cached.go", "package cache\r\n\r\n// AA OLDMATCH ZZ.\r\n"},
		{"cached.txt", "AA OLDMATCH ZZ.\r\n"},
	} {
		for _, edit := range []string{"unchanged", "same-length-rewrite", "bom-removed", "same-bytes-replacement"} {
			t.Run(fixture.name+"/"+edit, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, fixture.name)
				original := []byte("\xef\xbb\xbf" + fixture.body)
				if err := os.WriteFile(path, original, 0640); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				state, driver := testDriver(t)
				state.session.Root = root
				if !state.openFile(path) {
					t.Fatalf("open cached tab: %s", state.message)
				}
				cached := state.session.Current()
				selection := text.Range{Start: 2, End: 5}
				cached.Editor.SetSelection(selection)
				state.newDocument()
				active := state.session.Current()
				active.Replace("unrelated draft")
				active.Editor.SetSelection(text.Range{Start: 3, End: 7})
				draftSelection := active.Editor.Selection()
				recovery := state.session.RecoverySnapshot()
				query, disk := "OLDMATCH", original
				switch edit {
				case "same-length-rewrite":
					query, disk = "freshD12", bytes.ReplaceAll(original, []byte("OLDMATCH"), []byte("freshD12"))
				case "bom-removed":
					disk = []byte(fixture.body)
				case "same-bytes-replacement":
					replacement := filepath.Join(root, "replacement")
					if err := os.WriteFile(replacement, original, info.Mode().Perm()); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(replacement, info.Mode().Perm()); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(replacement, path); err != nil {
						t.Fatal(err)
					}
				}
				if edit == "same-length-rewrite" || edit == "bom-removed" {
					if err := os.WriteFile(path, disk, info.Mode().Perm()); err != nil {
						t.Fatal(err)
					}
				}
				state.openWorkspaceSearch()
				state.workspaceSearch.queryEditor.Insert(query)
				state.runWorkspaceSearch()
				awaitWorkspaceSearch(t, driver, state)
				if results := state.workspaceSearch.results; len(results) != 1 || !strings.Contains(results[0].Preview, query) {
					t.Fatalf("expected one verified disk hit for %q, got %+v", query, results)
				}
				mode, match := state.mode, state.html.workspaceMatch
				clickID(t, driver, workspaceResultRows.At(0))
				if actual, err := os.ReadFile(path); err != nil || !bytes.Equal(actual, disk) {
					t.Fatalf("search changed disk bytes: %q, %v", actual, err)
				}
				if actual, err := os.Stat(path); err != nil || actual.Mode().Perm() != info.Mode().Perm() {
					t.Fatalf("search changed file mode: %v, %v", actual, err)
				}
				if edit == "unchanged" {
					if state.session.Current() != cached || cached.Dirty() {
						t.Fatal("the valid hit did not activate the clean tab")
					}
					if cached.Kind == workspace.RichHTMLKind {
						if got := state.html.workspaceMatch; state.mode != renderedMode || got == nil || *got != (renderedMatch{block: 0, start: 3, end: 11}) {
							t.Fatalf("valid rich hit did not highlight the expected text: %+v", got)
						}
					} else {
						for range 3 {
							if driver.Snapshot().Focused == sourceID {
								break
							}
							if err := driver.Pump(); err != nil {
								t.Fatal(err)
							}
						}
						selected := cached.Editor.Selection()
						if state.mode != editMode || string([]rune(cached.Text())[selected.Start:selected.End]) != query || driver.Snapshot().Focused != sourceID {
							t.Fatalf("valid source hit did not select %q and focus the editor: %+v", query, selected)
						}
					}
					return
				}
				if state.session.Current() != active || state.mode != mode || state.html.workspaceMatch != match ||
					cached.Text() != fixture.body || cached.Editor.Selection() != selection || cached.Dirty() ||
					active.Editor.Selection() != draftSelection || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) ||
					!hasText(driver, workspace.ErrConflict.Error()) {
					t.Fatalf("hit did not refuse the stale tab without navigating: active changed=%v, selection=%+v, status=%q", state.session.Current() != active, cached.Editor.Selection(), state.message)
				}
			})
		}
	}
}

func TestWorkspaceSearchRejectsResultReplacedByOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("target\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Root = root
	state.openWorkspaceSearch()
	state.workspaceSearch.queryEditor.Insert("target")
	state.runWorkspaceSearch()
	awaitWorkspaceSearch(t, driver, state)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("target outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, workspaceResultRows.At(0))
	if state.session.Current().Path != "" || state.message != "搜索结果已过期，请重新搜索" {
		t.Fatalf("symlink swap navigated outside root: path=%q message=%q", state.session.Current().Path, state.message)
	}
}

func TestWorkspaceSearchOptionsAndNarrowSidebar(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"note.md": "Alpha42\n", "other.txt": "alpha43\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, workspaceOpenID)
	for _, id := range []gfx.ID{workspaceQueryID, workspaceCaseID, workspaceRegexID, workspaceOnlyID, workspaceRunID, workspaceStopID, workspaceBackID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				found = true
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 190 || hit.Bounds.Y+hit.Bounds.Height > 520 {
					t.Errorf("narrow control %v out of sidebar: %+v", id, hit.Bounds)
				}
			}
		}
		if !found {
			t.Fatalf("search control %v missing", id)
		}
	}
	clickID(t, driver, workspaceQueryID)
	if err := driver.Type("alpha"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, workspaceRunID)
	awaitWorkspaceSearch(t, driver, state)
	if len(state.workspaceSearch.results) != 2 {
		t.Fatalf("all-text results=%+v", state.workspaceSearch.results)
	}
	for index, result := range state.workspaceSearch.results {
		if result.Relative != "other.txt" {
			continue
		}
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == workspaceResultRows.At(index) {
				found = true
				if hit.Disabled {
					t.Fatal("plain text result was disabled despite its literal editor")
				}
			}
		}
		if !found {
			t.Fatal("plain text result was not visible")
		}
	}
	clickID(t, driver, workspaceCaseID)
	awaitWorkspaceSearch(t, driver, state)
	if len(state.workspaceSearch.results) != 1 || state.workspaceSearch.results[0].Relative != "other.txt" {
		t.Fatalf("case-sensitive results=%+v", state.workspaceSearch.results)
	}
	clickID(t, driver, workspaceOnlyID)
	awaitWorkspaceSearch(t, driver, state)
	if len(state.workspaceSearch.results) != 0 {
		t.Fatalf("Markdown-only case-sensitive results=%+v", state.workspaceSearch.results)
	}
	clickID(t, driver, workspaceCaseID)
	awaitWorkspaceSearch(t, driver, state)
	if len(state.workspaceSearch.results) != 1 || state.workspaceSearch.results[0].Relative != "note.md" {
		t.Fatalf("Markdown-only results=%+v", state.workspaceSearch.results)
	}
	clickID(t, driver, workspaceRegexID)
	awaitWorkspaceSearch(t, driver, state)
	if !state.workspaceSearch.query.Regex || state.workspaceSearch.error != "" {
		t.Fatalf("regex option state=%+v", state.workspaceSearch)
	}
	clickID(t, driver, workspaceBackID)
	if state.workspaceSearch.open {
		t.Fatal("return to file tree left content search open")
	}
	clickID(t, driver, searchID)
}

func TestWorkspaceSearchInvalidRegexShowsErrorWithoutOldHits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Root = root
	state.openWorkspaceSearch()
	state.workspaceSearch.queryEditor.Insert("alpha")
	state.runWorkspaceSearch()
	awaitWorkspaceSearch(t, driver, state)
	if len(state.workspaceSearch.results) != 1 {
		t.Fatal("valid search did not produce fixture result")
	}
	state.workspaceSearch.query.Regex = true
	state.workspaceSearch.queryEditor = text.NewEditor("[")
	state.runWorkspaceSearch()
	awaitWorkspaceSearch(t, driver, state)
	if state.workspaceSearch.error == "" || len(state.workspaceSearch.results) != 0 || state.workspaceSearch.searching {
		t.Fatalf("invalid regex state=%+v", state.workspaceSearch)
	}
}

func TestWorkspaceSearchCancelButtonDoesNotReportNoHits(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Root = t.TempDir()
	started := make(chan struct{})
	finished := make(chan struct{})
	state.workspaceSearch.worker = func(ctx context.Context, _ string, _ workspace.SearchOptions, _ func(workspace.SearchHit) bool) (workspace.SearchSummary, error) {
		close(started)
		<-ctx.Done()
		defer close(finished)
		return workspace.SearchSummary{}, ctx.Err()
	}
	state.openWorkspaceSearch()
	state.workspaceSearch.queryEditor.Insert("waiting")
	state.runWorkspaceSearch()
	<-started
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, workspaceStopID)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled worker still running")
	}
	if state.workspaceSearch.searching || state.workspaceSearch.error != "搜索已取消" || !hasText(driver, "搜索已取消") {
		t.Fatalf("canceled UI state=%+v", state.workspaceSearch)
	}
}

func awaitWorkspaceSearch(t *testing.T, driver *app.Driver, state *model) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for state.workspaceSearch.searching {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("workspace search did not complete")
		default:
			runtime.Gosched()
		}
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSearchRejectsDirtyMarkdownAlias(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("target\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "target"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 1 {
		t.Fatalf("search=%+v, %v", hits, err)
	}
	state := newModel()
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("本地草稿")
	state.session.Root = root
	state.openWorkspaceResult(hits[0])
	if state.message != "此文件有未保存修改，请在文内查找" || state.session.Current() != document || document.Text() != "本地草稿" || len(state.session.Tabs) != 2 {
		t.Fatalf("dirty alias result navigation: message=%q text=%q tabs=%d", state.message, document.Text(), len(state.session.Tabs))
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "target\n" {
		t.Fatalf("dirty alias result wrote source: %q, %v", data, err)
	}
}

func TestWorkspaceSearchDuplicateTextSelectsMatchingOffset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dup.md")
	content := "alpha\nbeta\nalpha\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	var hits []workspace.SearchHit
	if _, err := workspace.Search(context.Background(), root, workspace.SearchOptions{Query: "alpha"}, func(hit workspace.SearchHit) bool {
		hits = append(hits, hit)
		return true
	}); err != nil || len(hits) != 2 {
		t.Fatalf("duplicate search=%+v, %v", hits, err)
	}
	state, driver := testDriver(t)
	state.session.Root = root
	state.workspaceSearch.open = true
	state.workspaceSearch.results = hits
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, workspaceResultRows.At(1))
	current := state.session.Current()
	second := strings.Index(content, "alpha")
	second = second + strings.Index(content[second+1:], "alpha") + 1
	selection := current.Editor.Selection()
	if current.Path != hits[1].Path || selection.Start != second || selection.End != second+len("alpha") || state.mode != editMode {
		t.Fatalf("duplicate result selection path=%q selection=%+v want=%d mode=%v", current.Path, selection, second, state.mode)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != content {
		t.Fatalf("result navigation changed source: %q, %v", data, err)
	}
}
