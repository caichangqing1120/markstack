package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/assets"
	"markstack/internal/markdownedit"
	"markstack/internal/richedit"
	"markstack/internal/session"
	"markstack/internal/updater"
	"markstack/internal/workspace"
)

var (
	controlTree         = gfx.Tree{}
	page                = controlTree.Feature("markstack")
	fileRows            = controlTree.Sub(page, controlTree.Name("files"))
	rootSelectID        = controlTree.ID(page, "workspace-root-select")
	fileNewID           = controlTree.ID(page, "file-tree-new-file")
	folderNewID         = controlTree.ID(page, "file-tree-new-folder")
	treeRenameID        = controlTree.ID(page, "file-tree-rename")
	treeTrashID         = controlTree.ID(page, "file-tree-trash")
	treeNameID          = controlTree.ID(page, "file-tree-name")
	treeApplyID         = controlTree.ID(page, "file-tree-apply")
	treeCancelID        = controlTree.ID(page, "file-tree-cancel")
	treeTrashApproveID  = controlTree.ID(page, "file-tree-trash-approve")
	treeTrashCancelID   = controlTree.ID(page, "file-tree-trash-cancel")
	treeTrashScope      = controlTree.ID(page, "file-tree-trash-scope")
	treeTrashBackdropID = controlTree.ID(page, "file-tree-trash-backdrop")
	folderRows          = controlTree.Sub(page, controlTree.Name("folders"))
	workspaceResultRows = controlTree.Sub(page, controlTree.Name("workspace-results"))
	tabRows             = controlTree.Sub(page, controlTree.Name("tabs"))
	tabClose            = controlTree.Sub(page, controlTree.Name("tab-close"))
	outlineRows         = controlTree.Sub(page, controlTree.Name("outline"))
	previewLinks        = controlTree.Sub(page, controlTree.Name("preview-links"))
	previewImages       = controlTree.Sub(page, controlTree.Name("preview-images"))
	previewInlineImages = controlTree.Sub(page, controlTree.Name("preview-inline-images"))
	imageScope          = controlTree.ID(page, "image-scope")
	imageBackdropID     = controlTree.ID(page, "image-backdrop")
	imageCanvasID       = controlTree.ID(page, "image-canvas")
	imageZoomInID       = controlTree.ID(page, "image-zoom-in")
	imageZoomOutID      = controlTree.ID(page, "image-zoom-out")
	imageFitID          = controlTree.ID(page, "image-fit")
	imageCloseID        = controlTree.ID(page, "image-close")
	renderedRows        = controlTree.Sub(page, controlTree.Name("rendered-rows"))
	htmlLeafRows        = controlTree.Sub(page, controlTree.Name("html-leaves"))
	htmlBlocks          = controlTree.Sub(page, controlTree.Name("html-blocks"))
	htmlLinks           = controlTree.Sub(page, controlTree.Name("html-links"))
	htmlImages          = controlTree.Sub(page, controlTree.Name("html-images"))
	htmlInputID         = controlTree.ID(page, "html-input")
	taskChecks          = controlTree.Sub(page, controlTree.Name("task-checks"))
	renderedSelectionID = controlTree.ID(page, "rendered-selection")
	headingLevels       = controlTree.Sub(page, controlTree.Name("heading-levels"))
	blockFormats        = controlTree.Sub(page, controlTree.Name("block-formats"))
	headingMenuID       = controlTree.ID(page, "heading-menu")
	boldBlockID         = controlTree.ID(page, "bold-block")
	italicBlockID       = controlTree.ID(page, "italic-block")
	strikeBlockID       = controlTree.ID(page, "strike-block")
	linkBlockID         = controlTree.ID(page, "link-block")
	linkInputID         = controlTree.ID(page, "link-input")
	linkApplyID         = controlTree.ID(page, "link-apply")
	linkCancelID        = controlTree.ID(page, "link-cancel")
	findDocumentID      = controlTree.ID(page, "find-document")
	findInputID         = controlTree.ID(page, "find-input")
	replaceInputID      = controlTree.ID(page, "replace-input")
	findPreviousID      = controlTree.ID(page, "find-previous")
	findNextID          = controlTree.ID(page, "find-next")
	findCloseID         = controlTree.ID(page, "find-close")
	findCaseID          = controlTree.ID(page, "find-case")
	findRegexID         = controlTree.ID(page, "find-regex")
	findScopeID         = controlTree.ID(page, "find-scope")
	replaceOneID        = controlTree.ID(page, "replace-one")
	replaceAllID        = controlTree.ID(page, "replace-all")
	fileMenuScope       = controlTree.ID(page, "file-menu-scope")
	fileMenuID          = controlTree.ID(page, "file-menu")
	fileMenuBackdropID  = controlTree.ID(page, "file-menu-backdrop")
	menuNewID           = controlTree.ID(page, "menu-new")
	recentMenuID        = controlTree.ID(page, "menu-recent")
	recentScope         = controlTree.ID(page, "recent-scope")
	recentBackdropID    = controlTree.ID(page, "recent-backdrop")
	recentWorkspacesID  = controlTree.ID(page, "recent-workspaces")
	recentFilesID       = controlTree.ID(page, "recent-files")
	recentClearID       = controlTree.ID(page, "recent-clear")
	recentCloseID       = controlTree.ID(page, "recent-close")
	recentRows          = controlTree.Sub(page, controlTree.Name("recent-rows"))
	noteTemplateID      = controlTree.ID(page, "note-template")
	proposalTemplateID  = controlTree.ID(page, "proposal-template")
	newRichHTMLID       = controlTree.ID(page, "new-rich-html")

	openFolderID          = controlTree.ID(page, "open-folder")
	openFileID            = controlTree.ID(page, "open-file")
	newFileID             = controlTree.ID(page, "new")
	saveID                = controlTree.ID(page, "save")
	saveAsID              = controlTree.ID(page, "save-as")
	menuExportHTMLID      = controlTree.ID(page, "menu-export-html")
	menuExportPDFID       = controlTree.ID(page, "menu-export-pdf")
	menuSettingsID        = controlTree.ID(page, "menu-settings")
	settingsScope         = controlTree.ID(page, "settings-scope")
	settingsBackdropID    = controlTree.ID(page, "settings-backdrop")
	settingsCloseID       = controlTree.ID(page, "settings-close")
	appearanceLightID     = controlTree.ID(page, "appearance-light")
	appearanceDarkID      = controlTree.ID(page, "appearance-dark")
	sourceSizeDecreaseID  = controlTree.ID(page, "source-size-decrease")
	sourceSizeIncreaseID  = controlTree.ID(page, "source-size-increase")
	readingSizeDecreaseID = controlTree.ID(page, "reading-size-decrease")
	readingSizeIncreaseID = controlTree.ID(page, "reading-size-increase")
	readingStandardID     = controlTree.ID(page, "reading-standard")
	readingSoftID         = controlTree.ID(page, "reading-soft")
	autoSaveToggleID      = controlTree.ID(page, "auto-save-toggle")
	editModeID            = controlTree.ID(page, "edit")
	renderedModeID        = controlTree.ID(page, "rendered")
	richInputID           = controlTree.ID(page, "rich-input")
	codeInputID           = controlTree.ID(page, "code-input")
	tableInputID          = controlTree.ID(page, "table-input")
	renderedTableCells    = controlTree.Sub(page, controlTree.Name("rendered-table-cells"))
	splitModeID           = controlTree.ID(page, "split")
	readModeID            = controlTree.ID(page, "read")
	outlineToggleID       = controlTree.ID(page, "outline-toggle")
	sidebarToggleID       = controlTree.ID(page, "sidebar-toggle")
	sidebarResizeID       = controlTree.ID(page, "sidebar-resize")
	outlineResizeID       = controlTree.ID(page, "outline-resize")
	themeToggleID         = controlTree.ID(page, "theme-toggle")
	refreshID             = controlTree.ID(page, "refresh")
	tabPreviousID         = controlTree.ID(page, "previous-tab")
	tabNextID             = controlTree.ID(page, "next-tab")
	searchID              = controlTree.ID(page, "search")
	clearSearchID         = controlTree.ID(page, "clear-search")
	workspaceOpenID       = controlTree.ID(page, "workspace-search-open")
	workspaceBackID       = controlTree.ID(page, "workspace-search-back")
	workspaceQueryID      = controlTree.ID(page, "workspace-search-query")
	workspaceCaseID       = controlTree.ID(page, "workspace-search-case")
	workspaceRegexID      = controlTree.ID(page, "workspace-search-regex")
	workspaceOnlyID       = controlTree.ID(page, "workspace-search-markdown")
	workspaceRunID        = controlTree.ID(page, "workspace-search-run")
	workspaceStopID       = controlTree.ID(page, "workspace-search-stop")
	sourceID              = controlTree.ID(page, "source")
	confirmScope          = controlTree.ID(page, "confirm")
	confirmBackdropID     = controlTree.ID(page, "confirm-backdrop")
	saveConfirmID         = controlTree.ID(page, "confirm-save")
	discardConfirmID      = controlTree.ID(page, "confirm-discard")
	cancelConfirmID       = controlTree.ID(page, "confirm-cancel")
)

type viewMode uint8

const (
	editMode viewMode = iota
	renderedMode
	splitMode
	readMode
)

type outlineEntry struct {
	Title  string
	Level  int
	Rune   int
	start  int
	anchor string
}

type model struct {
	session                *session.Session
	confirmationShown      *session.Confirmation
	confirmationPress      *session.Confirmation
	confirmationButton     gfx.ID
	mode                   viewMode
	dark                   bool
	preferences            session.Preferences
	settingsStore          *session.PreferencesStore
	settingsOpen           bool
	settingsError          string
	settings               settingsPanel
	systemEnglish          bool
	updates                updatePanel
	sidebar                bool
	outline                bool
	sidebarWidth           float32
	outlineWidth           float32
	paneDragging           gfx.ID
	paneViewport           float32
	dropHeight             float32
	collapsed              map[string]bool
	search                 *text.Editor
	searchInput            app.TextInput
	findEditor             *text.Editor
	findInput              app.TextInput
	findOpen               bool
	findMatches            []int
	findEnds               []int
	findVisual             []renderedMatch
	findIndex              int
	findQuery              string
	findSource             string
	findDocument           int
	findDeferred           bool
	findPositionDeferred   bool
	findMode               viewMode
	findRect               ui.Rect
	findRegex              bool
	findCaseSensitive      bool
	findSelectedOnly       bool
	findAppliedRegex       bool
	findAppliedCase        bool
	findAppliedScope       bool
	findScope              text.Span
	findError              string
	replaceEditor          *text.Editor
	replaceInput           app.TextInput
	headingMenuOpen        bool
	linkEditor             *text.Editor
	slash                  slashMenu
	linkInput              app.TextInput
	linkOpen               bool
	linkRect               ui.Rect
	sourceInput            app.TextInput
	source                 components.CodeEditor
	codeHighlight          codeHighlightCache
	preview                previewView
	imageInspector         imageInspectorState
	markdownMediaSource    string
	markdownMediaPath      string
	markdownMediaID        int
	markdownMediaBlocks    map[int]markdownPreviewImage
	imageRenames           map[int][]imageRenameEdit
	pendingImageRenames    map[int][]imageRenameEdit
	deferredImageRollbacks []*session.Document
	markdownInlineBlocks   map[int][]inlinePreviewPart
	rich                   renderedEditor
	renderedDocuments      renderedDocumentHistories
	codeBlock              codeBlockEditor
	table                  tableEditor
	html                   richHTMLView
	selection              renderedSelection
	parsed                 markdown.Document
	compositionPreview     markdown.Document
	previewScroll          float32
	historyReveal          bool
	treeScroll             float32
	sourceRect             ui.Rect
	previewRect            ui.Rect
	treeRect               ui.Rect
	visible                []workspace.Node
	outlines               []outlineEntry
	message                string
	allowClose             func()
	invalidator            app.Invalidator
	tabStart               int
	focusRequest           gfx.ID
	focusReset             bool
	fileMenuOpen           bool
	fileMenuReturnFocus    fileMenuEditingFocus
	fileMenuScroll         components.MenuScroll
	chromeTooltip          components.HoverTooltip
	fileAction             fileActionKind
	fileActionTarget       string
	treeSelection          string
	treeName               *text.Editor
	treeNameInput          app.TextInput
	treeNameRect           ui.Rect
	fileTrashResult        workspace.TrashResult
	workspaceSearch        workspaceSearchState
	backlinks              backlinksPanel
	mediaManager           mediaManager
	diskWatch              diskWatch
	recoveryWriter         *recoveryWriter
	recoveryLoadErr        error
	recoveryWriteErr       error
	sessionWriter          *sessionWriter
	autoSave               autoSaveState
	recoveryLast           session.RecoverySnapshot
	sessionLast            session.SessionManifest
	recoveryDiscarded      bool
	quitDiscarded          bool
	recentStore            *session.RecentStore
	recents                session.RecentState
	recentOpen             bool
	recentShowFiles        bool

	outlinePanel          outlinePanel
	stats, selectionStats documentStats
	writing               writingMotion
	sourceShift           float32
	focusRuns             []components.CodeEditorRun
	anchor                scrollMotion
	pendingAnchor         int
	sourceAnchor          scrollMotion
	pendingSourceAnchor   int
	localLinks            localMarkdownLinks
	previewLinkHits       []markdownLinkHit
	linkPress             string
}

func newModel() *model {
	return &model{
		session: session.New(), sidebar: true, collapsed: make(map[string]bool), preferences: session.DefaultPreferences(),
		search: text.NewEditor(""), findEditor: text.NewEditor(""), replaceEditor: text.NewEditor(""), linkEditor: text.NewEditor(""), findCaseSensitive: true, focusRequest: sourceID,
		treeName:            text.NewEditor(""),
		workspaceSearch:     workspaceSearchState{queryEditor: text.NewEditor("")},
		outlinePanel:        outlinePanel{filter: text.NewEditor("")},
		pendingAnchor:       -1,
		pendingSourceAnchor: -1,
	}
}

func primaryModifier() gfx.Modifiers {
	if runtime.GOOS == "darwin" {
		return gfx.ModifierMeta
	}
	return gfx.ModifierControl
}

func newConfig(state *model) app.Config {
	config := app.Config{
		Tree: &controlTree, Title: "墨栈 MarkStack", Width: 1200, Height: 780,
		MinWidth: 740, MinHeight: 500, Invalidator: &state.invalidator,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: 32,
		Icon: assets.WindowIcon(),
	}
	config.Paint = state.paint
	config.OnWake = func() {
		state.drainWorkspaceSearch()
		state.drainBacklinks()
		state.drainMediaManager()
		state.drainDiskWatch()
		state.drainRecoveryErrors()
		state.drainSessionManifestErrors()
		state.drainAutoSave()
	}
	config.TextInput = func(context app.TextInputContext) *app.TextInput {
		switch context.ID {
		case sourceID:
			if current := state.session.Current(); current != nil {
				if current.ReadOnly() {
					state.sourceInput.Editor = nil
					state.sourceInput.State = app.TextStateOf(current.Editor)
					state.sourceInput.OnEvent = state.onReadOnlyTextEvent
					return &state.sourceInput
				}
				if current.Kind == workspace.MarkdownKind {
					state.sourceInput.Editor = nil
					state.sourceInput.State = app.TextStateOf(current.Editor)
					state.sourceInput.OnEvent = state.onMarkdownSourceTextEvent
				} else {
					state.sourceInput.Editor = current.Editor
					state.sourceInput.OnEvent = nil
				}
				return &state.sourceInput
			}
		case searchID:
			state.searchInput.Editor = state.search
			return &state.searchInput
		case outlineFilterID:
			if state.outline && state.outlinePanel.filterRect.Width > 0 {
				state.outlinePanel.input.Editor = state.outlinePanel.filter
				return &state.outlinePanel.input
			}
		case workspaceQueryID:
			if state.workspaceSearch.open {
				state.workspaceSearch.input.Editor = state.workspaceSearch.queryEditor
				return &state.workspaceSearch.input
			}
		case treeNameID:
			if state.fileAction == fileActionCreateFile || state.fileAction == fileActionCreateFolder || state.fileAction == fileActionRename {
				state.treeNameInput.Editor = state.treeName
				return &state.treeNameInput
			}
		case findInputID:
			if state.findOpen {
				state.findInput.Editor = state.findEditor
				return &state.findInput
			}
		case replaceInputID:
			if state.findOpen && state.sourceReplaceAvailable() {
				state.replaceInput.Editor = state.replaceEditor
				return &state.replaceInput
			}
		case linkInputID:
			if state.linkOpen {
				state.linkInput.Editor = state.linkEditor
				return &state.linkInput
			}
		case imageNameID:
			if state.imageInspector.open && state.imageInspector.rename != nil {
				state.imageInspector.nameInput.Editor = state.imageInspector.rename
				return &state.imageInspector.nameInput
			}
		case richInputID:
			if state.mode == renderedMode && state.rich.active {
				state.rich.textInput.Editor = nil
				state.rich.textInput.State = app.TextStateOf(state.rich.input)
				state.rich.textInput.OnEvent = state.onRenderedTextEvent
				state.rich.textInput.HideNativeOverlays = true
				return &state.rich.textInput
			}
		case codeInputID:
			if state.mode == renderedMode && state.codeBlock.active {
				state.codeBlock.textInput.Editor = nil
				state.codeBlock.textInput.State = app.TextStateOf(state.codeBlock.input)
				state.codeBlock.textInput.OnEvent = state.onCodeBlockTextEvent
				state.codeBlock.textInput.HideNativeOverlays = true
				return &state.codeBlock.textInput
			}
		case codeLanguageInputID:
			if state.mode == renderedMode && state.codeBlock.active && state.codeBlock.languageInput != nil {
				state.codeBlock.languageText.Editor = state.codeBlock.languageInput
				return &state.codeBlock.languageText
			}
		case tableInputID:
			if state.mode == renderedMode && state.table.active {
				state.table.textInput.Editor = nil
				state.table.textInput.State = app.TextStateOf(state.table.input)
				state.table.textInput.OnEvent = state.onTableCellTextEvent
				return &state.table.textInput
			}
		case htmlInputID:
			if state.mode == renderedMode && state.html.active && state.html.input != nil {
				if _, composing := state.html.input.Composition(); !composing || state.html.textInput.Bounds == (ui.Rect{}) {
					state.html.textInput.Bounds = state.html.inputRect
				}
				state.html.textInput.Editor = nil
				state.html.textInput.State = state.html.nativeTextState()
				state.html.textInput.OnEvent = state.onHTMLTextEvent
				return &state.html.textInput
			}
		}
		return nil
	}
	config.OnKey = state.onKey
	config.OnPointer = state.onPointer
	config.OnFileDrop = state.onFileDrop
	config.OnScroll = state.onScroll
	config.OnShortcut = state.onShortcut
	config.OnCloseRequest = state.requestClose
	config.OnClosed = func() {
		state.updates.close()
		state.stopDiskWatch()
		state.backlinks.scan.stop()
		state.closeMediaManager()
	}
	return config
}

func (state *model) requestClose(allow func()) {
	if state.updates.installing {
		return
	}
	state.closeMediaManager()
	state.diskWatch.confirm = nil
	state.imageInspector = imageInspectorState{}
	state.settingsOpen = false
	state.fileMenuOpen = false
	state.recentOpen = false
	if state.session.Pending() != nil {
		return
	}
	state.quitDiscarded = false
	if state.requestSessionQuit() {
		state.cancelWorkspaceSearch()
		allow()
		return
	}
	state.allowClose = allow
	state.invalidator.Invalidate()
}

func (state *model) onKey(event gfx.KeyEvent) gfx.EventResult {
	if state.mediaManager.open || state.diskWatch.confirm != nil {
		return gfx.EventResult{}
	}
	if state.fileMenuOpen {
		compositionFocus := state.pendingCompositionFocus()
		// Marked text owns the keyboard. Moving menu focus or dismissing the
		// menu here would deactivate the native input before its IME can finish.
		if event.Key == gfx.KeyEscape && compositionFocus == (gfx.ID{}) {
			state.fileMenuOpen = false
			return gfx.EventResult{Handled: true, Repaint: true, Focus: fileMenuID}
		}
		if compositionFocus == (gfx.ID{}) {
			if target, handled := state.fileMenuScroll.Navigate(event.Key, event.Target, true); handled {
				return gfx.EventResult{Handled: true, Repaint: true, Focus: target}
			}
		}
	}
	if state.settingsOpen {
		if event.Key == gfx.KeyEscape {
			state.dismissSettings()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	if state.imageInspector.open {
		return state.onImageInspectorKey(event)
	}
	if state.slash.documentID != 0 {
		if result := state.onSlashKey(event); result.Handled {
			return result
		}
	}
	if state.session.Pending() != nil {
		if focus := state.confirmationCompositionFocus(); focus != (gfx.ID{}) && event.Key == gfx.KeyTab {
			return gfx.EventResult{Handled: true, Focus: focus}
		}
		return gfx.EventResult{}
	}
	if result := state.onPaneKey(event); result.Handled {
		return result
	}
	if state.fileAction == fileActionTrashConfirm && event.Key == gfx.KeyEscape {
		state.cancelFileAction()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Target == treeNameID && state.fileAction != fileActionNone {
		switch event.Key {
		case gfx.KeyEnter:
			state.applyFileAction()
			return gfx.EventResult{Handled: true, Repaint: true}
		case gfx.KeyEscape:
			state.cancelFileAction()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: searchID}
		}
	}
	if state.headingMenuOpen && event.Key == gfx.KeyEscape {
		state.headingMenuOpen = false
		return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	}
	if event.Target == findInputID {
		switch event.Key {
		case gfx.KeyEnter:
			direction := 1
			if event.Modifiers&gfx.ModifierShift != 0 {
				direction = -1
			}
			state.stepDocumentFind(direction)
			return gfx.EventResult{Handled: true, Repaint: true}
		case gfx.KeyEscape:
			return gfx.EventResult{Handled: true, Repaint: true, Focus: state.closeDocumentFind()}
		}
	}
	if event.Target == replaceInputID {
		switch event.Key {
		case gfx.KeyEnter:
			state.replaceDocumentMatches(false)
			return gfx.EventResult{Handled: true, Repaint: true, Focus: replaceInputID}
		case gfx.KeyEscape:
			return gfx.EventResult{Handled: true, Repaint: true, Focus: state.closeDocumentFind()}
		}
	}
	if event.Target == workspaceQueryID && state.workspaceSearch.open {
		switch event.Key {
		case gfx.KeyEnter:
			state.runWorkspaceSearch()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: workspaceQueryID}
		case gfx.KeyEscape:
			state.closeWorkspaceSearch()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: searchID}
		}
	}
	if event.Target == linkInputID && state.linkOpen {
		switch event.Key {
		case gfx.KeyEnter:
			state.applyLinkBlock()
			focus := linkInputID
			if !state.linkOpen {
				focus = richInputID
			}
			return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
		case gfx.KeyEscape:
			state.linkOpen = false
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
	}
	if event.Target == newFileID && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		state.newDocument()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
	}
	if index, ok := event.Target.In(outlineRows); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && state.navigateOutline(index) {
		focus := gfx.ID{}
		switch {
		case state.mode == editMode:
			focus = sourceID
		case state.rich.active:
			focus = richInputID
		case state.codeBlock.active:
			focus = codeInputID
		case state.table.active:
			focus = tableInputID
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
	}
	if index, ok := event.Target.In(renderedRows); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		return state.activateRendered(index)
	}
	if index, ok := event.Target.In(technicalPreviewRows); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		return state.activateTechnicalPreview(index)
	}
	if index, ok := event.Target.In(tableActions); ok && index < len(tableActionButtons) && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		return state.applyTableAction(markdownedit.TableAction(index), false)
	}
	if index, ok := event.Target.In(taskChecks); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		return state.toggleTaskAt(index)
	}
	if event.Target == codeLanguageInputID {
		switch event.Key {
		case gfx.KeyEnter:
			return state.finishCodeLanguage(true)
		case gfx.KeyEscape:
			return state.finishCodeLanguage(false)
		}
	}
	if event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace {
		if result := state.onCodeControl(event.Target); result.Handled {
			return result
		}
	}
	if index, ok := event.Target.In(htmlLeafRows); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		return state.activateHTMLLeaf(index)
	}
	if index, ok := event.Target.In(htmlBlocks); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		return state.inspectHTMLBlock(index)
	}
	if event.Target == renderedSelectionID && event.Key == gfx.KeyEscape && state.selection.active {
		return state.collapseRenderedSelection()
	}
	if event.Target == renderedSelectionID && state.selection.active && event.Modifiers&gfx.ModifierShift != 0 {
		index := state.selection.extentIndex
		switch event.Key {
		case gfx.KeyUp:
			if state.selection.anchorVirtual != nil && index == state.selection.anchorIndex {
				return state.collapseRenderedSelection()
			}
			return state.extendRenderedSelection(index - 1)
		case gfx.KeyDown:
			if state.selection.anchorVirtual != nil && index == state.selection.anchorIndex-1 {
				return state.collapseRenderedSelection()
			}
			return state.extendRenderedSelection(index + 1)
		}
	}
	if event.Target == codeInputID && state.codeBlock.active && event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 {
		state.indentFencedCode(event.Modifiers&gfx.ModifierShift != 0)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
	}
	if event.Target == codeInputID && state.codeBlock.active && state.codeBlock.input != nil && !state.codeBlockComposing() {
		movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
		if movement != components.TextareaMoveNone {
			input := state.codeBlock.input
			value := input.String()
			offset := state.codeBlock.view.Move(value, byteOffsetOfRune(value, input.Caret()), movement)
			input.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
			return gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
		}
	}
	if event.Target == richInputID && state.mode == renderedMode {
		if event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 {
			if result := state.indentRenderedList(event.Modifiers&gfx.ModifierShift != 0); result.Handled {
				return result
			}
		}
		if event.Key == gfx.KeyEnter {
			if result := state.enterM2Marker(); result.Handled {
				return result
			}
			return state.insertRenderedNewline()
		}
		if state.rich.moveVisualEdge(event) {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
		if state.rich.moveVisualRow(event.Key, event.Modifiers&gfx.ModifierShift != 0) {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
		index := state.rich.index
		switch event.Key {
		case gfx.KeyUp:
			index--
		case gfx.KeyDown:
			if !state.virtualParagraphNeedsPlaceholder() {
				index++
			}
		default:
			return gfx.EventResult{}
		}
		if event.Modifiers&gfx.ModifierShift != 0 {
			return state.selectRendered(index)
		}
		return state.activateRendered(index)
	}
	if event.Target == tableInputID && state.table.active {
		if event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 {
			return state.navigateTableCell(event.Modifiers&gfx.ModifierShift != 0)
		}
		if event.Key == gfx.KeyEnter {
			if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
				return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
			}
			return state.tableCellSourceFallback(state.session.Current(), state.table.cell.End)
		}
		if !state.tableComposing() {
			movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
			switch movement {
			case components.TextareaMoveLineStart, components.TextareaMoveTextStart:
				state.table.input.MoveTo(0, extend)
				return gfx.EventResult{Handled: true, Repaint: true}
			case components.TextareaMoveLineEnd, components.TextareaMoveTextEnd:
				state.table.input.MoveTo(state.table.input.RuneCount(), extend)
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
	}
	if event.Target == htmlInputID && state.mode == renderedMode {
		if event.Key == gfx.KeyEnter {
			return state.insertHTMLParagraph()
		}
		if event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 {
			if result := state.navigateHTMLTable(event.Modifiers&gfx.ModifierShift != 0); result.Handled {
				return result
			}
		}
		if state.html.moveKey(event) {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
		}
	}
	if event.Target != sourceID || state.mode == readMode {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 && state.sourceReplaceAvailable() {
		state.indentSource(event.Modifiers&gfx.ModifierShift != 0)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	if movement == components.TextareaMoveNone {
		return gfx.EventResult{}
	}
	current := state.session.Current()
	value := current.Text()
	to := state.source.Move(value, byteOffsetOfRune(value, current.Editor.Caret()), movement)
	current.Editor.MoveTo(utf8.RuneCountInString(value[:to]), extend)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) onPointer(event gfx.PointerEvent) gfx.EventResult {
	if state.mediaManager.open || state.diskWatch.confirm != nil {
		return gfx.EventResult{}
	}
	if (event.Target == diskReloadID || event.Target == diskIgnoreID || event.Target == diskSaveAsID) && event.Kind == gfx.PointerDown {
		if focus := state.pendingCompositionFocus(); !focus.IsZero() {
			return gfx.EventResult{Focus: focus}
		}
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary {
		state.linkPress = ""
		state.confirmationPress = nil
		switch event.Target {
		case cancelConfirmID, discardConfirmID, saveConfirmID:
			state.confirmationPress = state.confirmationShown
			state.confirmationButton = event.Target
		}
	}
	if state.settingsOpen {
		return gfx.EventResult{}
	}
	if state.imageInspector.open {
		return state.onImageInspectorPointer(event)
	}
	if state.slash.documentID != 0 && event.Kind == gfx.PointerDown {
		_, item := event.Target.In(slashItems)
		_, size := event.Target.In(slashSize)
		if item || size {
			return gfx.EventResult{Focus: richInputID}
		}
		if event.Target != richInputID {
			state.slash = slashMenu{}
		}
	}
	if focus := state.confirmationCompositionFocus(); focus != (gfx.ID{}) {
		return gfx.EventResult{Focus: focus}
	}
	if focus := state.fileMenuCompositionFocus(event.Target); focus != (gfx.ID{}) {
		return gfx.EventResult{Focus: focus}
	}
	if focus := state.formatControlCompositionFocus(event.Target); focus != (gfx.ID{}) {
		return gfx.EventResult{Focus: focus}
	}
	if result := state.onHTMLPointer(event); result.Handled || !result.Focus.IsZero() {
		return result
	}
	if result := state.onPanePointer(event); result.Handled {
		return result
	}
	if state.session.Pending() != nil || event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}
	}
	if !state.fileMenuOpen && !state.recentOpen && state.fileAction != fileActionTrashConfirm {
		if result := state.onMarkdownLinkPointer(event); result.Handled {
			return result
		}
	}
	if index, ok := event.Target.In(tableActions); ok && index < len(tableActionButtons) && state.table.active {
		if event.Kind == gfx.PointerDown {
			return gfx.EventResult{Focus: tableInputID}
		}
		if event.Kind == gfx.PointerUp {
			return state.applyTableAction(markdownedit.TableAction(index), false)
		}
	}
	if event.Kind == gfx.PointerDown && event.Target == richInputID && state.rich.active {
		state.rich.dragAnchorX, state.rich.dragAnchorY, state.rich.placeDragAnchor = event.X, event.Y, true
		return gfx.EventResult{}
	}
	if event.Kind != gfx.PointerUp {
		return gfx.EventResult{}
	}
	if index, ok := event.Target.In(taskChecks); ok {
		return state.toggleTaskAt(index)
	}
	if result := state.onCodeControl(event.Target); result.Handled {
		return result
	}
	if index, ok := event.Target.In(technicalPreviewRows); ok {
		return state.activateTechnicalPreview(index)
	}
	closedHeadingMenu := false
	if state.headingMenuOpen && event.Target != headingMenuID {
		_, headingOption := event.Target.In(headingLevels)
		_, blockOption := event.Target.In(blockFormats)
		if !headingOption && !blockOption {
			state.headingMenuOpen = false
			closedHeadingMenu = true
		}
	}
	if event.Target == richInputID && state.rich.active {
		if index := state.renderedRowAt(event.Y); index >= 0 && (index != state.rich.index || state.virtualParagraphNeedsPlaceholder()) {
			state.rich.input.MoveTo(state.rich.dragAnchor, false)
			return state.selectRenderedAt(index, event.X, event.Y)
		}
	}
	if event.Target == newFileID {
		state.newDocument()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
	}
	if index, ok := event.Target.In(outlineRows); ok && state.navigateOutline(index) {
		focus := gfx.ID{}
		switch {
		case state.mode == editMode:
			focus = sourceID
		case state.rich.active:
			focus = richInputID
		case state.codeBlock.active:
			focus = codeInputID
		case state.table.active:
			focus = tableInputID
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
	}
	if index, ok := event.Target.In(renderedRows); ok {
		if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
		}
		if event.Modifiers&gfx.ModifierShift != 0 && state.rich.active {
			return state.selectRenderedAt(index, event.X, event.Y)
		}
		result := state.activateRendered(index)
		if state.rich.active {
			state.rich.clickX, state.rich.clickY, state.rich.placeCaret = event.X, event.Y, true
		}
		return result
	}
	if index, ok := event.Target.In(renderedTableCells); ok {
		return state.activateTableCell(index)
	}
	return gfx.EventResult{Repaint: closedHeadingMenu}
}

// fileMenuCompositionFocus keeps popup pointer focus on the composing input.
func (state *model) fileMenuCompositionFocus(target gfx.ID) gfx.ID {
	focus := state.pendingCompositionFocus()
	if focus == (gfx.ID{}) {
		return gfx.ID{}
	}
	if target == fileMenuID || target == fileMenuBackdropID || state.fileMenuOpen {
		return focus
	}
	return gfx.ID{}
}

func (state *model) navigateOutline(index int) bool {
	if index < 0 || index >= len(state.outlines) {
		return false
	}
	if state.session.Current().Kind == workspace.RichHTMLKind {
		if state.mode == editMode {
			state.session.Current().Editor.MoveTo(state.outlines[index].Rune, false)
			return true
		}
		heading := 0
		for blockIndex, block := range state.html.parsed.Blocks {
			if block.Kind != richedit.Heading {
				continue
			}
			if heading == index && blockIndex < len(state.html.blockY) {
				state.html.scroll = max(0, min(state.html.blockY[blockIndex]-80, max(0, state.html.height-state.html.rect.Height)))
				state.html.active = false
				return true
			}
			heading++
		}
		return false
	}
	if state.mode != editMode {
		targetStart := byteOffsetOfRune(state.session.Current().Text(), state.outlines[index].Rune)
		for blockIndex := range state.parsed.Blocks {
			block := &state.parsed.Blocks[blockIndex]
			if block.Kind == markdown.Heading && block.Start == targetStart && block.Level == state.outlines[index].Level && renderedPlain(&state.parsed, blockIndex) == state.outlines[index].Title && blockIndex < len(state.preview.blocks) {
				// Marked text owns the keyboard until it is committed: refuse the
				// jump before scrolling, or the composing editor leaves the
				// viewport and cannot keep the focus it still owns.
				if state.pendingCompositionFocus() != (gfx.ID{}) {
					return true
				}
				state.previewScroll = max(0, state.preview.blocks[blockIndex].top-8)
				if state.mode == renderedMode {
					// A rendered outline jump must leave native input focused so
					// the very next keystroke edits the target heading.
					state.activateRendered(blockIndex)
				} else {
					state.deactivateRenderedView(state.session.Current())
				}
				return true
			}
		}
	}
	if state.pendingCompositionFocus() != (gfx.ID{}) {
		return true
	}
	state.mode = editMode
	state.session.Current().Editor.MoveTo(state.outlines[index].Rune, false)
	return true
}

func (state *model) onScroll(event gfx.ScrollEvent) gfx.EventResult {
	if panel := &state.mediaManager; panel.open {
		if !panel.confirming && contains(panel.rows, ui.Point{X: event.X, Y: event.Y}) {
			panel.scroll = max(0, min(panel.scroll+event.DeltaY, panel.extent))
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{Handled: true}
	}
	if state.diskWatch.confirm != nil {
		return gfx.EventResult{Handled: true}
	}
	if state.fileMenuOpen {
		return gfx.EventResult{Handled: true, Repaint: state.fileMenuScroll.Wheel(event)}
	}
	if state.settingsOpen {
		return gfx.EventResult{Handled: true}
	}
	if state.imageInspector.open {
		return state.onImageInspectorScroll(event)
	}
	if state.session.Pending() != nil || state.recentOpen {
		return gfx.EventResult{Handled: true}
	}
	point := ui.Point{X: event.X, Y: event.Y}
	if current := state.session.Current(); current != nil && current.Kind == workspace.RichHTMLKind && state.mode != editMode && contains(state.html.rect, point) {
		state.html.scroll = max(0, min(state.html.scroll+event.DeltaY, max(0, state.html.height-state.html.rect.Height)))
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	switch {
	case contains(state.backlinks.rows, point):
		state.backlinks.scroll += event.DeltaY
	case contains(state.outlinePanel.rowsRect, point):
		state.outlinePanel.scroll += event.DeltaY
	case contains(state.workspaceSearch.resultsRect, point):
		state.workspaceSearch.scroll = max(0, min(state.workspaceSearch.scroll+event.DeltaY, max(0, state.workspaceSearch.resultsHeight-state.workspaceSearch.resultsRect.Height)))
	case contains(state.sourceRect, point):
		if state.sourceShift != 0 {
			halfHeight := state.sourceRect.Height / 2
			state.setSourceWritingScroll(max(-halfHeight, min(state.sourceWritingScroll()+event.DeltaY, state.source.MaxScrollY()+halfHeight)))
		} else {
			state.source.ScrollBy(0, event.DeltaY)
		}
		state.pendingSourceAnchor = -1
		state.writing.tracking = false
		state.outlinePanel.followViewport = true
		state.outlinePanel.followPreview = false
	case contains(state.previewRect, point):
		minimum, maximum := state.previewLimits(state.previewRect)
		state.previewScroll = max(minimum, min(state.previewScroll+event.DeltaY, maximum))
		state.writing.tracking = false
		state.anchor.active = false
		state.outlinePanel.followViewport = true
		state.outlinePanel.followPreview = true
	case contains(state.treeRect, point):
		state.treeScroll = max(0, min(state.treeScroll+event.DeltaY, max(0, float32(len(state.visible))*27-state.treeRect.Height)))
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) onShortcut(event gfx.KeyEvent) gfx.EventResult {
	if state.mediaManager.open || state.diskWatch.confirm != nil {
		if event.Modifiers == 0 {
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true}
	}
	if state.settingsOpen {
		return gfx.EventResult{Handled: true}
	}
	if state.imageInspector.open {
		if event.Target == imageNameID {
			return gfx.EventResult{}
		}
		if event.Text == " " && event.Modifiers == 0 {
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true}
	}
	if state.session.Pending() != nil || state.recentOpen {
		return gfx.EventResult{Handled: true}
	}
	if state.fileAction == fileActionTrashConfirm {
		return gfx.EventResult{Handled: true}
	}
	if event.Target == treeNameID && state.fileAction != fileActionNone {
		return gfx.EventResult{}
	}
	if result := state.onRenderedDocumentHistoryShortcut(event); result.Handled {
		return result
	}
	modifiers := event.Modifiers
	primary := primaryModifier()
	key := strings.ToLower(event.Text)
	if modifiers == primary && len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		index := int(key[0] - '1')
		if index < len(state.session.Tabs) && index != state.session.Active && state.prepareDocumentNavigation() {
			state.session.Active = index
			state.resetDocumentView()
			state.fileMenuOpen = false
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if modifiers == primary|gfx.ModifierAlt && (key == "1" || key == "2" || key == "3") {
		if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
		}
		state.fileMenuOpen = false
		current := state.session.Current()
		switch key {
		case "1":
			state.mode = editMode
			return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
		case "2":
			if current.Kind == workspace.MarkdownKind && !current.ReadOnly() {
				state.mode = splitMode
			}
		case "3":
			if !current.ReadOnly() && (current.Kind == workspace.MarkdownKind || current.Kind == workspace.RichHTMLKind) {
				state.mode = readMode
			}
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if modifiers == primary|gfx.ModifierAlt && (key == "t" || key == "d") {
		state.toggleWritingMode(key == "t")
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if key == "a" && modifiers == primary && event.Target == codeInputID {
		return state.selectFencedCode()
	}
	if event.Target == tableInputID && modifiers == primary|gfx.ModifierShift {
		switch key {
		case "!":
			key = "1"
		case "@":
			key = "2"
		case "#":
			key = "3"
		case ")":
			key = "0"
		}
		for index, action := range tableActionButtons {
			if key == action.key {
				return state.applyTableAction(markdownedit.TableAction(index), false)
			}
		}
	}
	if key == "t" && modifiers == primary|gfx.ModifierShift {
		if index, ok := event.Target.In(taskChecks); ok {
			return state.toggleTaskAt(index)
		}
		if event.Target == richInputID && state.rich.active && state.rich.block.Task {
			return state.toggleTaskAt(state.rich.index)
		}
	}
	if state.mode == renderedMode && state.html.crossSelection != nil && modifiers == primary {
		selection := state.html.crossSelection
		switch key {
		case "c":
			return gfx.EventResult{Handled: true, ClipboardText: selection.visible}
		case "x":
			if err := state.applyHTMLCrossSelection(""); err != nil {
				state.message = err.Error()
			}
			return gfx.EventResult{Handled: true, Repaint: true, ClipboardText: selection.visible, Focus: htmlInputID}
		case "v":
			value, err := app.ClipboardText()
			if err == nil && value != "" {
				err = state.applyHTMLCrossSelection(value)
			}
			if err != nil {
				state.message = err.Error()
			}
			return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
		}
	}
	if modifiers == primary|gfx.ModifierAlt && key == "f" {
		state.openDocumentFind()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: findInputID}
	}
	if modifiers == primary|gfx.ModifierShift && key == "f" {
		state.openWorkspaceSearch()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: workspaceQueryID}
	}
	if state.mode == renderedMode && state.selection.active && modifiers == primary {
		switch key {
		case "c":
			return gfx.EventResult{Handled: true, ClipboardText: state.selection.visible}
		case "x":
			selection := state.selection
			if !selection.editable {
				state.message = "选区包含只读块，请在 Markdown 源码模式编辑"
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			if err := state.session.Current().EditRange(selection.start, selection.end, selection.prefix+selection.suffix); err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			result := state.finishRenderedSelection(selection, "")
			result.ClipboardText = selection.visible
			return result
		case "v":
			if !state.selection.editable {
				state.message = "选区包含只读块，请在 Markdown 源码模式编辑"
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			value, err := app.ClipboardText()
			if err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			if value == "" {
				return gfx.EventResult{Handled: true}
			}
			if err := state.session.Current().EditRange(state.selection.start, state.selection.end, state.selection.prefix+value+state.selection.suffix); err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			return state.finishRenderedSelection(state.selection, value)
		}
	}
	if event.Target == richInputID && state.mode == renderedMode && state.rich.active && !state.linkOpen && !state.findOpen && !state.selection.active && modifiers == primary {
		if focus := state.pendingFormatCompositionFocus(); focus != (gfx.ID{}) && (key == "b" || key == "i" || key == "k" || key == "`" || key == "/") {
			state.message = session.ErrCompositionPending.Error()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
		}
		if key == "b" || key == "i" || key == "`" || key == "k" {
			state.prepareInlineShortcut()
		}
		switch key {
		case "/":
			return state.startSlashMenu(true)
		case "b":
			state.applyBoldBlock()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		case "i":
			state.applyItalicBlock()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		case "`":
			state.applyWholeBlockStyle("`")
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		case "k":
			if state.rich.block.Kind == markdownedit.Paragraph {
				return gfx.EventResult{Handled: true, Repaint: true, Focus: state.openLinkShortcut()}
			}
		}
	}
	fromFileMenu := state.fileMenuOpen
	if modifiers == primary|gfx.ModifierShift && key == "s" {
		state.fileMenuOpen = false
		if fromFileMenu {
			state.saveFromFileMenu(true)
		} else {
			state.saveActive(true)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if modifiers != primary {
		return gfx.EventResult{}
	}
	state.fileMenuOpen = false
	switch key {
	case "n":
		state.newDocument()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
	case "o":
		state.openFiles()
	case "s":
		if fromFileMenu {
			state.saveFromFileMenu(false)
		} else {
			state.saveActive(false)
		}
	case "f":
		state.findOpen = false
		state.sidebar = true
		return gfx.EventResult{Handled: true, Repaint: true, Focus: searchID}
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) openFolder() {
	root, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: state.local("选择工作区", "Choose workspace")})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	changed, err := state.session.RequestWorkspace(root)
	if err != nil {
		state.message = err.Error()
	} else if changed {
		state.cancelWorkspaceSearch()
		state.collapsed = make(map[string]bool)
		state.resetDocumentView()
		state.recordRecentWorkspace(state.session.Root)
	}
}

func (state *model) openFiles() {
	if !state.prepareDocumentNavigation() {
		return
	}
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: state.local("打开文档", "Open document")})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	if err := state.session.OpenMany(paths); err != nil {
		state.message = err.Error()
		return
	}
	state.resetDocumentView()
	if state.session.Current().Kind == workspace.RichHTMLKind {
		state.mode = renderedMode
	}
	state.recordRecentFiles(paths)
}

func (state *model) openFile(path string) bool {
	if !state.prepareDocumentNavigation() {
		return false
	}
	if _, err := state.session.Open(path); err != nil {
		state.message = err.Error()
		return false
	}
	state.resetDocumentView()
	if state.session.Current().Kind == workspace.RichHTMLKind {
		state.mode = renderedMode
	}
	state.recordRecentFile(state.session.Current().Path)
	return true
}

func (state *model) saveActive(choosePath bool) {
	state.saveActiveWithPicker(choosePath, app.SaveFile)
}

func (state *model) saveActiveWithPicker(choosePath bool, saveFile func(app.SaveFileOptions) (string, error)) {
	current := state.session.Current()
	if current == nil {
		return
	}
	if current.ReadOnly() {
		state.message = largePreviewText
		return
	}
	if state.documentComposing(current) {
		state.message = session.ErrCompositionPending.Error()
		if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
			state.focusRequest = focus
			state.invalidator.Invalidate()
		}
		return
	}
	if choosePath && current.Path != "" && len(state.pendingImageRenames[current.ID]) > 0 {
		state.message = state.local("当前文档包含未保存的图片重命名，请先保存或撤销后再另存为", "Save or undo pending image renames before Save As")
		return
	}
	path := ""
	if choosePath || current.Path == "" {
		var err error
		path, err = saveFile(app.SaveFileOptions{
			Title: state.local("保存文档", "Save document"), SuggestedName: current.Title, DefaultExtension: saveExtension(current),
		})
		if errors.Is(err, app.ErrCanceled) {
			return
		}
		if err != nil {
			state.message = err.Error()
			return
		}
	}
	if err := state.session.Save(state.session.Active, path); err != nil {
		state.message = err.Error()
		if current.Path != "" && errors.Is(err, workspace.ErrConflict) {
			state.observeDisk(current, true)
		}
	} else {
		state.commitPendingImageRenames(state.session.Current().ID)
		state.message = state.local("已保存 ", "Saved ") + state.session.Current().Title
		state.recordRecentFile(state.session.Current().Path)
	}
}

func (state *model) resolve(decision session.Decision) {
	state.resolveWithSavePicker(decision, app.SaveFile)
}

func (state *model) resolveWithSavePicker(decision session.Decision, saveFile func(app.SaveFileOptions) (string, error)) {
	confirmation := state.session.Pending()
	if confirmation == nil {
		return
	}
	if decision == session.Save && confirmation.Document == state.session.Current() && state.documentComposing(confirmation.Document) {
		state.message = session.ErrCompositionPending.Error()
		return
	}
	path := ""
	if decision == session.Save && confirmation.Document.Path == "" {
		var err error
		path, err = saveFile(app.SaveFileOptions{
			Title: state.local("保存文档", "Save document"), SuggestedName: confirmation.Document.Title, DefaultExtension: saveExtension(confirmation.Document),
		})
		if errors.Is(err, app.ErrCanceled) {
			return
		}
		if err != nil {
			state.message = err.Error()
			return
		}
		if pending := state.session.Pending(); pending == nil || *pending != *confirmation {
			state.message = state.local("待保存文档已变化，请重新确认", "The document changed. Confirm again before saving.")
			return
		}
	}
	previousRoot := state.session.Root
	completed, err := state.session.Resolve(decision, path)
	if err != nil {
		state.message = err.Error()
		return
	}
	if decision == session.Save && confirmation.Document.Path != "" {
		state.commitPendingImageRenames(confirmation.Document.ID)
		state.recordRecentFile(confirmation.Document.Path)
	}
	if decision == session.Discard {
		state.deferredImageRollbacks = append(state.deferredImageRollbacks, confirmation.Document)
	}
	if decision == session.Cancel {
		state.deferredImageRollbacks = nil
		state.allowClose = nil
		state.quitDiscarded = false
		if state.updates.quitPending {
			state.updates.quitPending = false
			state.openSettingsSection(settingsAbout)
		}
		return
	}
	if confirmation.Action == session.Quit && decision == session.Discard {
		state.quitDiscarded = true
	}
	if !completed {
		return
	}
	if len(state.deferredImageRollbacks) > 0 {
		for _, doc := range state.deferredImageRollbacks {
			state.rollbackPendingImageRenames(previousRoot, doc)
		}
		state.deferredImageRollbacks = nil
	}
	if confirmation.Action == session.SwitchWorkspace {
		state.cancelWorkspaceSearch()
		state.collapsed = make(map[string]bool)
		state.resetDocumentView()
		state.recordRecentWorkspace(state.session.Root)
	}
	if confirmation.Action == session.Quit {
		if !state.updates.quitPending {
			state.finishQuitRecovery()
		}
		if state.allowClose != nil {
			state.cancelWorkspaceSearch()
			allow := state.allowClose
			state.allowClose = nil
			allow()
		}
	}
}

func (state *model) finishQuitRecovery() {
	state.recoveryDiscarded = state.quitDiscarded
	state.scheduleRecovery()
}

func saveExtension(document *session.Document) string {
	switch document.Kind {
	case workspace.RichHTMLKind:
		return "mh"
	case workspace.PlainKind:
		return "txt"
	case workspace.CodeKind:
		if extension := strings.TrimPrefix(filepath.Ext(document.Path), "."); extension != "" {
			return extension
		}
		return "go"
	}
	return "md"
}

func (state *model) prepareDocumentNavigation() bool {
	if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
		state.focusRequest = focus
		return false
	}
	if current := state.session.Current(); current != nil {
		state.syncRendered(current)
		state.syncCodeBlock(current)
		state.syncTableCell(current)
	}
	return true
}

func (state *model) resetDocumentView() {
	state.storeRenderedDocumentHistory()
	if current := state.session.Current(); current == nil || current.Editor != state.renderedDocuments.editor.Value() {
		state.preview = previewView{}
		state.parsed = markdown.Document{}
		state.compositionPreview = markdown.Document{}
	}
	state.source = components.CodeEditor{}
	state.codeHighlight = codeHighlightCache{}
	state.markdownMediaSource = ""
	state.markdownMediaPath = ""
	state.markdownMediaID = 0
	state.markdownMediaBlocks = nil
	state.markdownInlineBlocks = nil
	state.imageInspector = imageInspectorState{}
	state.rich = renderedEditor{}
	state.table = tableEditor{}
	state.codeBlock = codeBlockEditor{}
	state.html = richHTMLView{}
	state.selection = renderedSelection{}
	state.headingMenuOpen = false
	state.findOpen = false
	state.findMatches = nil
	state.findEnds = nil
	state.findVisual = nil
	state.findDocument = 0
	state.findDeferred = false
	state.findPositionDeferred = false
	state.previewScroll = 0
	state.writing = writingMotion{}
	state.sourceShift = 0
	state.anchor = scrollMotion{}
	state.pendingAnchor = -1
	state.pendingSourceAnchor = -1
	state.sourceAnchor = scrollMotion{}
	state.linkPress = ""
	state.previewLinkHits = nil
	state.historyReveal = false
	state.message = ""
	if current := state.session.Current(); current != nil && (current.Kind == workspace.CodeKind || current.Kind == workspace.PlainKind) {
		state.mode = editMode
	}
	state.restoreRenderedDocumentHistory()
}

func (state *model) newDocument() {
	state.fileMenuOpen = false
	state.session.NewDocument()
	state.mode = editMode
	state.resetDocumentView()
}

func (state *model) newFromTemplate(kind session.Template) {
	state.fileMenuOpen = false
	if _, err := state.session.NewFromTemplate(kind); err != nil {
		state.message = err.Error()
		return
	}
	state.mode = editMode
	state.resetDocumentView()
	state.deferEditingFocus()
}

func (state *model) newRichHTML() {
	state.fileMenuOpen = false
	document := state.session.NewRichHTML()
	state.mode = renderedMode
	state.resetDocumentView()
	state.html.sync(document)
	state.focusRequest = state.activateHTMLLeafAt(0, 0).Focus
	state.invalidator.Invalidate()
}

func (state *model) toggleTheme() {
	next := state.preferences
	next.Dark = !state.dark
	state.changeSettings(next)
}

func (state *model) deferEditingFocus() {
	state.focusRequest, state.focusReset = sourceID, true
}

func (state *model) refresh() {
	if state.session.Root == "" {
		return
	}
	nodes, err := workspace.Scan(state.session.Root)
	if err != nil {
		state.message = err.Error()
		return
	}
	state.session.Nodes = nodes
	state.html.clearImages()
	state.markdownMediaPath = ""
	state.markdownMediaBlocks = nil
	state.markdownInlineBlocks = nil
	state.message = state.local("文件树已刷新", "File tree refreshed")
}

func byteOffsetOfRune(value string, index int) int {
	for offset := range value {
		if index == 0 {
			return offset
		}
		index--
	}
	return len(value)
}

func contains(rect ui.Rect, point ui.Point) bool {
	return rect.Width > 0 && point.X >= rect.X && point.X < rect.X+rect.Width && point.Y >= rect.Y && point.Y < rect.Y+rect.Height
}

func runApp(state *model) error {
	return app.RunApplication(func(application *app.Application) error {
		application.OnQuitRequest(state.requestClose)
		state.updates.quit = application.Quit
		var canvas gfx.Canvas
		_, err := application.OpenWindow(newConfig(state), &canvas)
		if err == nil {
			state.startUpdateCheckOnLaunch()
		}
		return err
	})
}

func main() {
	if handled, err := updater.RunPatchHelper(os.Args[1:]); handled {
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	state := newModel()
	state.systemEnglish = prefersEnglish(app.PreferredLanguages())
	defer func() {
		if err := state.stopSessionManifest(); err != nil {
			log.Printf("session manifest: %v", err)
		}
	}()
	if preferencesEnabled() {
		if store, err := defaultPreferencesStore(); err != nil {
			state.message = state.local("设置不可用：", "Settings unavailable: ") + err.Error()
			log.Print(state.message)
		} else if err := state.enablePreferences(store); err != nil {
			state.message = state.settingsErrorText(err)
			log.Print(state.message)
		}
	}
	state.configureUpdates()
	if recoveryEnabled() {
		store, err := defaultRecoveryStore()
		if err == nil {
			err = state.enableRecovery(store)
		}
		if err != nil {
			state.recoveryLoadErr = err
			log.Printf("恢复快照不可用：%v", err)
		}
	}
	defer func() {
		if err := state.stopRecovery(); err != nil {
			log.Printf("recovery journal: %v", err)
		}
	}()
	defer state.stopAutoSave()
	if recentHistoryEnabled() {
		if store, err := defaultRecentStore(); err != nil {
			state.message = "最近记录不可用：" + err.Error()
			log.Print(state.message)
		} else if err := state.enableRecents(store); err != nil {
			state.message = "最近记录不可用：" + err.Error()
			log.Print(state.message)
		}
	}
	if sessionManifestEnabled() {
		if store, err := defaultSessionStore(); err != nil {
			state.message = "会话快照不可用：" + err.Error()
			log.Print(state.message)
		} else if err := state.enableSessionManifest(store, len(os.Args) == 1 || len(state.session.RecoverySnapshot().Drafts) > 0); err != nil {
			state.message = "会话快照不可用：" + err.Error()
			log.Print(state.message)
		}
	}
	if len(os.Args) > 1 {
		root, err := filepath.Abs(os.Args[1])
		if err == nil {
			var changed bool
			changed, err = state.session.RequestWorkspace(root)
			if changed {
				state.recordRecentWorkspace(state.session.Root)
			}
		}
		if err != nil {
			log.Fatal(err)
		}
	}
	if err := runApp(state); err != nil {
		log.Fatal(err)
	}
}
