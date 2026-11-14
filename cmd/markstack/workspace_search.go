package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

type workspaceSearchEvent struct {
	generation uint64
	hit        workspace.SearchHit
	summary    workspace.SearchSummary
	err        error
	done       bool
}

type workspaceSearchState struct {
	worker        func(context.Context, string, workspace.SearchOptions, func(workspace.SearchHit) bool) (workspace.SearchSummary, error)
	events        chan workspaceSearchEvent
	cancel        context.CancelFunc
	generation    uint64
	results       []workspace.SearchHit
	query         workspace.SearchOptions
	searching     bool
	truncated     bool
	error         string
	open          bool
	queryEditor   *text.Editor
	input         app.TextInput
	rect          ui.Rect
	scroll        float32
	resultsHeight float32
	resultsRect   ui.Rect
}

func (state *model) openWorkspaceSearch() {
	state.sidebar = true
	state.workspaceSearch.open = true
	state.focusRequest, state.focusReset = workspaceQueryID, true
	state.invalidator.Invalidate()
}

func (state *model) closeWorkspaceSearch() {
	state.cancelWorkspaceSearch()
	state.workspaceSearch.open = false
	state.workspaceSearch.rect = ui.Rect{}
	state.workspaceSearch.resultsRect = ui.Rect{}
	state.focusRequest, state.focusReset = searchID, true
	state.invalidator.Invalidate()
}

func (state *model) runWorkspaceSearch() {
	options := state.workspaceSearch.query
	options.Query = state.workspaceSearch.queryEditor.String()
	state.startWorkspaceSearch(state.session.Root, options)
	state.workspaceSearch.scroll = 0
	state.invalidator.Invalidate()
}

func (state *model) openWorkspaceResult(hit workspace.SearchHit) {
	kind := workspace.KindOf(hit.Path)
	if kind == workspace.UnsupportedKind {
		return
	}
	root, err := filepath.EvalSymlinks(state.session.Root)
	if err != nil {
		state.message = err.Error()
		return
	}
	relative, err := filepath.Rel(root, hit.Path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		state.message = "搜索结果不在当前工作区"
		return
	}
	location, err := workspace.CanonicalLocation(hit.Path)
	if err != nil {
		state.message = err.Error()
		return
	}
	for _, document := range state.session.Tabs {
		if (document.Path == hit.Path || document.SavedLocation() == location) && document.Dirty() {
			state.message = "此文件有未保存修改，请在文内查找"
			return
		}
	}
	snapshot, err := workspace.Load(hit.Path)
	if err != nil || sha256.Sum256(snapshot.Bytes) != hit.Fingerprint {
		state.message = "搜索结果已过期，请重新搜索"
		return
	}
	state.openFile(hit.Path)
	current := state.session.Current()
	if current == nil || current.SavedLocation() != snapshot.Location() {
		return
	}
	if kind == workspace.RichHTMLKind {
		state.mode = renderedMode
		if !hit.FileName {
			state.html.sync(current)
			if !hit.Rich || hit.RichBlock < 0 || hit.RichBlock >= len(state.html.parsed.Blocks) || hit.RichStart < 0 || hit.RichEnd < hit.RichStart || hit.RichEnd > len(state.html.parsed.Blocks[hit.RichBlock].Visible) {
				state.message = "搜索结果已过期，请重新搜索"
				return
			}
			state.html.workspaceMatch = &renderedMatch{block: hit.RichBlock, start: hit.RichStart, end: hit.RichEnd}
			state.html.scrollBlock, state.html.scrollPending = hit.RichBlock, true
		}
		state.invalidator.Invalidate()
		return
	}
	start, end := hit.ByteOffset, hit.ByteOffset+hit.MatchBytes
	if hit.FileName {
		start, end = 0, 0
	}
	value := current.Text()
	if start < 0 || end > len(value) || !utf8.ValidString(value[:start]) || !utf8.ValidString(value[:end]) {
		state.message = "搜索结果已过期，请重新搜索"
		return
	}
	current.Editor.SetSelection(text.Range{Start: utf8.RuneCountInString(value[:start]), End: utf8.RuneCountInString(value[:end])})
	state.mode = editMode
	state.deferEditingFocus()
}

func (state *model) startWorkspaceSearch(root string, options workspace.SearchOptions) {
	state.cancelWorkspaceSearch()
	search := &state.workspaceSearch
	search.query = options
	if root == "" {
		search.error = "请先打开工作区"
		return
	}
	if search.events == nil {
		search.events = make(chan workspaceSearchEvent, 64)
	}
	ctx, cancel := context.WithCancel(context.Background())
	search.cancel = cancel
	search.searching = true
	generation, events := search.generation, search.events
	worker := search.worker
	if worker == nil {
		worker = workspace.Search
	}
	go func() {
		publish := func(hit workspace.SearchHit) bool {
			select {
			case <-ctx.Done():
				return false
			case events <- workspaceSearchEvent{generation: generation, hit: hit}:
				state.invalidator.Invalidate()
				return true
			}
		}
		summary, err := worker(ctx, root, options, publish)
		select {
		case <-ctx.Done():
			return
		case events <- workspaceSearchEvent{generation: generation, summary: summary, err: err, done: true}:
			state.invalidator.Invalidate()
		}
	}()
}

func (state *model) cancelWorkspaceSearch() {
	search := &state.workspaceSearch
	if search.cancel != nil {
		search.cancel()
		search.cancel = nil
	}
	search.generation++
	search.searching = false
	search.truncated = false
	search.error = ""
	search.results = search.results[:0]
}

func (state *model) drainWorkspaceSearch() {
	search := &state.workspaceSearch
	if search.events == nil {
		return
	}
	for range 128 {
		select {
		case event := <-search.events:
			if event.generation != search.generation {
				continue
			}
			if event.done {
				search.searching = false
				search.cancel = nil
				search.truncated = event.summary.Truncated
				if event.err != nil && !errors.Is(event.err, context.Canceled) {
					search.error = event.err.Error()
				}
				continue
			}
			if len(search.results) < 500 {
				search.results = append(search.results, event.hit)
			}
		default:
			return
		}
	}
	state.invalidator.Invalidate()
}
