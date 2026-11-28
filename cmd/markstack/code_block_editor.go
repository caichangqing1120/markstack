package main

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type codeBlockEditor struct {
	active             bool
	documentID         int
	renderedIndex      int
	sourceIndex        int
	source             string
	block              markdownedit.Block
	input              *text.Editor
	textInput          app.TextInput
	view               components.CodeEditor
	languageInput      *text.Editor
	languageText       app.TextInput
	languageQuery      string
	languageOptions    []string
	languagePopover    ui.Rect
	suggestionViewport ui.Rect
	selectedSuggestion int
	suggestionScroll   float32
	emptySelected      bool
}

const (
	codeHeaderHeight         = 32
	codeLanguageRowHeight    = 25
	codeLanguageMaxRows      = 9
	codeLanguageSearchHeight = 36
)

type codeCopyFeedback struct {
	documentID, copiedBlockIndex int
	source                       string
	expires                      time.Duration
}

var (
	codeLanguageButtons     = controlTree.Sub(page, controlTree.Name("code-language-buttons"))
	codeCopyButtons         = controlTree.Sub(page, controlTree.Name("code-copy-buttons"))
	codeExitButtons         = controlTree.Sub(page, controlTree.Name("code-exit-buttons"))
	codeDeleteButtons       = controlTree.Sub(page, controlTree.Name("code-delete-buttons"))
	codeLineNumberButtons   = controlTree.Sub(page, controlTree.Name("code-line-number-buttons"))
	codeWrapButtons         = controlTree.Sub(page, controlTree.Name("code-wrap-buttons"))
	codeLanguageInputID     = controlTree.ID(page, "code-language-input")
	codeLanguageSuggestions = controlTree.Sub(page, controlTree.Name("code-language-suggestions"))
	codeLanguageBackdropID  = controlTree.ID(page, "code-language-backdrop")
)

func (state *model) codeBlockComposing() bool {
	if state.codeBlock.input == nil {
		return false
	}
	_, composing := state.codeBlock.input.Composition()
	return composing
}

func (state *model) activateCodeBlock(index int) gfx.EventResult {
	current := state.session.Current()
	if current == nil || state.mode != renderedMode || index < 0 || index >= len(state.parsed.Blocks) || state.parsed.Blocks[index].Kind != markdown.Code {
		return gfx.EventResult{}
	}
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	if !state.flushCardDraft() {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	sourceIndex := specialSourceBlockIndex(current.Text(), &state.parsed, index, blocks)
	if sourceIndex < 0 {
		rendered := state.parsed.Blocks[index]
		offset := rendered.Start
		if rendered.Start >= 0 && rendered.Start < rendered.End && rendered.End <= len(current.Text()) {
			offset = min(rendered.End, sourceLineEnd(current.Text(), rendered.Start)+1)
		}
		return state.codeBlockSourceFallback(current, offset)
	}
	block := blocks[sourceIndex]
	state.deactivateRenderedView(current)
	state.table = tableEditor{}
	state.selection = renderedSelection{}
	state.codeBlock = codeBlockEditor{
		active: true, documentID: current.ID, renderedIndex: index, sourceIndex: sourceIndex,
		source: current.Text(), block: block, input: text.NewEditor(block.Code.Visible),
	}
	state.codeBlock.input.MoveTo(utf8.RuneCountInString(block.Code.Visible), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
}

func (state *model) codeBlockSourceFallback(current *session.Document, offset int) gfx.EventResult {
	state.codeBlock = codeBlockEditor{}
	state.mode = editMode
	state.message = "此块请在 Markdown 源码模式编辑"
	state.focusRequest = sourceID
	offset = max(0, min(offset, len(current.Text())))
	current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:offset]), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func (state *model) onCodeBlockTextEvent(event app.TextEvent) (app.TextState, error) {
	input := state.codeBlock.input
	if input == nil {
		return app.TextState{}, nil
	}
	if event.Kind == app.TextCommandEvent && event.Command == app.TextSelectAll {
		state.selectFencedCode()
		return app.TextStateOf(input), nil
	}
	state.codeBlock.emptySelected = false
	if event.Kind == app.TextCommandEvent && (event.Command == app.TextUndo || event.Command == app.TextRedo) {
		if _, composing := input.Composition(); composing {
			return app.TextStateOf(input), nil
		}
		state.historyReveal = true
		return state.stepCodeBlockHistory(event.Command), nil
	}
	if event.Kind == app.TextCommandEvent && !state.codeBlockComposing() {
		switch event.Command {
		case app.TextInsertNewline:
			state.insertCodeNewline()
			return renderedHistoryInputState(state.codeBlock.input), nil
		case app.TextDeleteBackward, app.TextDeleteForward:
			if result := state.revertEmptyCodeBlock(); result.Handled {
				next := renderedHistoryInputState(state.codeBlock.input)
				next.Redraw = result.Repaint
				return next, nil
			}
		}
	}
	event.Apply(input)
	return app.TextStateOf(input), nil
}

func (state *model) selectFencedCode() gfx.EventResult {
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		result.Focus = focus
		return result
	}
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || !code.active || code.documentID != current.ID || code.source != current.Text() || code.input == nil {
		return gfx.EventResult{}
	}
	if code.input.RuneCount() == 0 && !code.emptySelected {
		code.emptySelected = true
		return result
	}
	if code.input.Selection() != (text.Range{End: code.input.RuneCount()}) {
		code.input.SetSelection(text.Range{End: code.input.RuneCount()})
		return result
	}
	state.deactivateRenderedView(current)
	state.codeBlock = codeBlockEditor{}
	state.mode, state.focusRequest = editMode, sourceID
	current.Editor.SetSelection(text.Range{End: current.Editor.RuneCount()})
	result.Focus = sourceID
	return result
}

func (state *model) openCodeLanguage(index int) gfx.EventResult {
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	if !state.codeBlock.active || state.codeBlock.renderedIndex != index {
		result := state.activateCodeBlock(index)
		if !state.codeBlock.active {
			return result
		}
	}
	if state.codeBlock.block.Code.Math {
		return gfx.EventResult{}
	}
	code := &state.codeBlock
	code.languageInput = text.NewEditor("")
	code.languageOptions, code.suggestionScroll = nil, 0
	if code.renderedIndex >= 0 {
		state.historyReveal = true
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: codeLanguageInputID}
}

func (state *model) finishCodeLanguage(apply bool) gfx.EventResult {
	if state.codeBlock.languageInput == nil {
		return gfx.EventResult{}
	}
	language := strings.TrimSpace(state.codeBlock.languageInput.String())
	if apply {
		if options := state.fenceLanguageSuggestions(); len(options) > 0 {
			language = options[state.codeBlock.selectedSuggestion]
		}
	}
	return state.applyCodeLanguage(apply, language)
}

func (state *model) applyCodeLanguage(apply bool, language string) gfx.EventResult {
	code, current := &state.codeBlock, state.session.Current()
	if code.languageInput == nil || current == nil || !code.active || code.documentID != current.ID || code.source != current.Text() {
		return gfx.EventResult{}
	}
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
	if !apply {
		code.languageInput = nil
		return result
	}
	if workspace.NormalizeCodeLanguage(language) == "plaintext" {
		language = ""
	}
	patch, err := markdownedit.PatchFenceLanguage([]byte(code.source), code.block, language)
	if err != nil {
		state.message = "无效的代码语言标识"
		result.Focus = codeLanguageInputID
		return result
	}
	source := code.source
	if patch.Text != source[patch.Start:patch.End] {
		bodyStart := code.block.Code.Start
		selection := directedCodeSelection(code.input)
		before := codeSelectionInSource(source, bodyStart, code.input.String(), selection)
		next := source[:patch.Start] + patch.Text + source[patch.End:]
		updatedStart := bodyStart + len(patch.Text) - (patch.End - patch.Start)
		after := codeSelectionInSource(next, updatedStart, code.input.String(), selection)
		if err := applyMarkdownPatch(current, patch, before, after); err != nil {
			state.message = err.Error()
			result.Focus = codeLanguageInputID
			return result
		}
		blocks := markdownedit.Parse([]byte(next))
		code.block, code.source = blocks[code.sourceIndex], next
		state.moveInactiveRenderedHistory(current, source, 1, true)
	}
	code.languageInput = nil
	state.message = ""
	return result
}

func codeSelectionInSource(source string, bodyStart int, visible string, selection text.Range) text.Range {
	base := utf8.RuneCountInString(source[:bodyStart])
	if bodyStart < 2 || source[bodyStart-2] != '\r' {
		return shiftedSelection(selection, base)
	}
	position := func(offset int) int {
		return base + offset + strings.Count(visible[:byteOffsetOfRune(visible, offset)], "\n")
	}
	return text.Range{Start: position(selection.Start), End: position(selection.End)}
}

func (state *model) copyCode(index int) gfx.EventResult {
	if index < 0 || index >= len(state.parsed.Blocks) || state.parsed.Blocks[index].Kind != markdown.Code {
		return gfx.EventResult{}
	}
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	value := state.parsed.Blocks[index].Text
	if err := app.SetClipboardText(value); err != nil {
		state.message = err.Error()
	} else {
		state.message = "已复制代码"
		current := state.session.Current()
		state.codeCopy = codeCopyFeedback{documentID: current.ID, copiedBlockIndex: index, source: current.Text()}
		state.showToast("已复制代码块", "Copied code block")
	}
	result := gfx.EventResult{Handled: true, Repaint: true}
	if state.codeBlock.active && state.codeBlock.renderedIndex == index {
		result.Focus = codeInputID
	}
	return result
}

func (state *model) onCodeControl(target gfx.ID) gfx.EventResult {
	if index, ok := target.In(codeCopyButtons); ok {
		return state.copyCode(index)
	}
	if index, ok := target.In(codeLanguageButtons); ok {
		return state.openCodeLanguage(index)
	}
	if index, ok := target.In(codeLanguageSuggestions); ok {
		options := state.fenceLanguageSuggestions()
		if index >= 0 && index < len(options) {
			return state.applyCodeLanguage(true, options[index])
		}
	}
	if index, ok := target.In(codeExitButtons); ok && state.codeBlock.active && state.codeBlock.renderedIndex == index {
		return state.exitCodeBlockToParagraph()
	}
	if index, ok := target.In(codeDeleteButtons); ok && state.codeBlock.active && state.codeBlock.renderedIndex == index {
		return state.deleteCodeBlock(false)
	}
	_, lines := target.In(codeLineNumberButtons)
	_, wrap := target.In(codeWrapButtons)
	if lines || wrap {
		if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
			return gfx.EventResult{Handled: true, Focus: focus}
		}
		state.changeFormatPreference(func(next *session.Preferences) {
			if lines {
				next.HideCodeLineNumbers = !next.HideCodeLineNumbers
			}
			if wrap {
				next.DisableCodeWrap = !next.DisableCodeWrap
			}
		})
		result := gfx.EventResult{Handled: true, Repaint: true}
		if state.codeBlock.active {
			result.Focus = codeInputID
		}
		return result
	}
	return gfx.EventResult{}
}

func (state *model) fenceLanguageSuggestions() []string {
	code := &state.codeBlock
	input := code.languageInput
	if input == nil {
		return nil
	}
	if _, composing := input.Composition(); composing {
		return nil
	}
	query := strings.TrimSpace(input.String())
	if code.languageOptions == nil || query != code.languageQuery {
		options := workspace.CodeLanguageSuggestions(query, 50)
		if query == "" {
			options = append([]string{"plaintext"}, options...)
		}
		code.languageQuery, code.languageOptions = query, options
		code.selectedSuggestion, code.suggestionScroll = 0, 0
	}
	return code.languageOptions
}

func (state *model) paintCodeControls(canvas *gfx.Canvas, theme ui.Theme, palette colors, clip ui.Rect, x, y float32, blocks []markdownedit.Block) {
	source := state.session.Current().Text()
	var anchor ui.Rect
	for index := state.preview.firstVisible(clip.Y - y); index < len(state.preview.blocks); index++ {
		layout := &state.preview.blocks[index]
		top := y + layout.top
		if top >= clip.Y+clip.Height {
			break
		}
		if layout.kind != markdown.Code || top+codeHeaderHeight <= clip.Y || layout.width < 90 {
			continue
		}
		active := state.codeBlock.active && state.codeBlock.renderedIndex == index
		if !active && (layout.vector.Width > 0 || layout.mathFallback) {
			continue
		}
		right := x + layout.width - 6
		control := func(id gfx.ID, controlWidth float32, value, label string, toggled, highlighted bool) {
			right -= controlWidth
			paintCodeControl(canvas, palette, id, ui.Rect{X: right, Y: top + 4, Width: controlWidth, Height: 24}, value, label, toggled, highlighted)
			right -= 3
		}
		copied := state.codeWasCopied(canvas, index, source)
		caption := state.local("复制", "Copy")
		if copied {
			caption = state.local("✓ 已复制", "✓ Copied")
		}
		copyWidth := float32(64)
		if layout.width < 160 {
			copyWidth = 32
			if copied {
				caption = "✓"
			}
		}
		control(codeCopyButtons.At(index), copyWidth, caption, state.local("复制代码", "Copy code"), false, copied)
		if active {
			if layout.width >= 380 {
				control(codeDeleteButtons.At(index), 44, state.local("删除", "Delete"), state.local("删除代码块", "Delete code block"), false, false)
			}
			exitCaption, exitWidth := state.local("↓ 新段落", "↓ Paragraph"), float32(82)
			if layout.width < 320 {
				exitCaption, exitWidth = "↓", 26
			}
			control(codeExitButtons.At(index), exitWidth, exitCaption, state.local("下方新段落", "Paragraph below"), false, false)
		}
		editable := state.mode == renderedMode && !state.preview.displayMath[index] && specialSourceBlockIndex(source, &state.parsed, index, blocks) >= 0
		if layout.width >= 280 && (editable || active) {
			control(codeWrapButtons.At(index), 40, state.local("换行", "Wrap"), state.local("自动换行", "Word wrap"), true, !state.preferences.DisableCodeWrap)
			lineCaption, lineWidth := "#", float32(28)
			if layout.width >= 360 {
				lineCaption, lineWidth = state.local("行号", "Lines"), 40
			}
			control(codeLineNumberButtons.At(index), lineWidth, lineCaption, state.local("行号", "Line numbers"), true, !state.preferences.HideCodeLineNumbers)
		}
		tag := markdownedit.FenceLanguage(state.parsed.Blocks[index].Info)
		language := state.codeLanguageName(tag)
		if editable {
			if tag == "" {
				language = state.local("选择语言…", "Choose language…")
			}
			language += " ▾"
		}
		font := palette.textSize(11)
		field := ui.Rect{X: x + 7, Y: top + 5, Width: min(right-x-7, canvas.TextWidthWeight(language, font, gfx.WeightSemibold)+16), Height: 22}
		if field.Width <= 0 {
			continue
		}
		id, fill, border, ink := codeLanguageButtons.At(index), palette.sidebar, palette.border, palette.ink
		if editable && (tag == "" || canvas.Hovered() == id || canvas.Focused() == id || active && state.codeBlock.languageInput != nil) {
			fill, border, ink = palette.selected, palette.accent, palette.accent
		}
		canvas.RoundedBorder(field, field.Height/2, 1, border, fill)
		canvas.LabelCenter(ellipsizeWeight(canvas, language, field.Width-8, font, gfx.WeightSemibold), field, gfx.TextStyle{Size: font, Weight: gfx.WeightSemibold, Color: ink})
		if editable {
			canvas.HitWithOptions(id, field, state.local("代码语言：", "Code language: ")+language, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: active && state.codeBlock.languageInput != nil})
		}
		if active {
			anchor = field
		}
	}
	state.paintCodeLanguagePopover(canvas, theme, palette, anchor, clip)
}

func (state *model) codeWasCopied(canvas *gfx.Canvas, index int, source string) bool {
	feedback := &state.codeCopy
	if feedback.documentID != state.session.Current().ID || feedback.copiedBlockIndex != index || feedback.source != source {
		return false
	}
	now := canvas.Now()
	if feedback.expires == 0 {
		feedback.expires = now + toastDuration
	}
	if now >= feedback.expires {
		*feedback = codeCopyFeedback{}
		return false
	}
	canvas.RequestFrameIn(feedback.expires - now)
	return true
}

func (state *model) codeLanguageName(tag string) string {
	if tag == "" || workspace.NormalizeCodeLanguage(tag) == "plaintext" {
		return state.local("纯文本", "Plain text")
	}
	return workspace.CodeLanguageDisplayName(tag)
}

func (state *model) paintCodeLanguagePopover(canvas *gfx.Canvas, theme ui.Theme, palette colors, anchor, clip ui.Rect) {
	code := &state.codeBlock
	if code.languageInput == nil || clip.Height < codeLanguageSearchHeight || clip.Width < 16 {
		return
	}
	options := state.fenceLanguageSuggestions()
	const gap = 5
	width := min(float32(248), clip.Width-2*gap)
	below := max(clip.Y+gap, min(anchor.Y+anchor.Height+gap, clip.Y+clip.Height-gap))
	above := max(clip.Y+gap, min(anchor.Y-gap, clip.Y+clip.Height-gap))
	wanted := float32(codeLanguageSearchHeight + min(len(options), codeLanguageMaxRows)*codeLanguageRowHeight + gap)
	belowRoom, aboveRoom := clip.Y+clip.Height-gap-below, above-clip.Y-gap
	room := belowRoom
	showAbove := wanted > belowRoom && aboveRoom > belowRoom
	if showAbove {
		room = aboveRoom
	}
	box := ui.Rect{X: min(max(clip.X+gap, anchor.X), clip.X+clip.Width-width-gap), Y: below, Width: width, Height: min(wanted, room)}
	if showAbove {
		box.Y = above - box.Height
	}
	if box.Height < codeLanguageSearchHeight+gap {
		box.Y, box.Height = clip.Y+gap, min(wanted, clip.Height-2*gap)
	}
	code.languagePopover = box
	canvas.PushLayer(8)
	defer canvas.PopLayer()
	canvas.HitWithOptions(codeLanguageBackdropID, clip, "", gfx.HitOptions{SkipFocus: true})
	canvas.Elevate(box, 8, gfx.ShadowSM)
	canvas.RoundedBorder(box, 8, 1, palette.border, palette.background)
	field := ui.Rect{X: box.X + gap, Y: box.Y + gap, Width: box.Width - 2*gap, Height: codeLanguageSearchHeight - 2*gap}
	theme.Metrics.FontSizeM = 12
	components.PaintInput(canvas, theme, field, components.InputSnapshot{ID: codeLanguageInputID, Label: state.local("代码语言", "Code language"), Value: code.languageInput.String(), Placeholder: state.local("搜索 48+ 种语言…", "Search 48+ languages…"), Focused: canvas.Focused() == codeLanguageInputID, PlatformEditing: true, NavigationKeys: true})
	list := ui.Rect{X: field.X, Y: box.Y + codeLanguageSearchHeight, Width: field.Width, Height: max(0, box.Height-codeLanguageSearchHeight-gap)}
	code.suggestionViewport = list
	if list.Height == 0 || len(options) == 0 {
		return
	}
	extent := float32(len(options) * codeLanguageRowHeight)
	code.suggestionScroll = min(code.suggestionScroll, max(0, extent-list.Height))
	current := workspace.NormalizeCodeLanguage(markdownedit.FenceLanguage(code.block.Code.Info))
	if current == "" {
		current = "plaintext"
	}
	canvas.Clip(list)
	for index := int(code.suggestionScroll / codeLanguageRowHeight); index < len(options); index++ {
		bounds := ui.Rect{X: list.X, Y: list.Y + float32(index*codeLanguageRowHeight) - code.suggestionScroll, Width: list.Width - 7, Height: codeLanguageRowHeight}
		if bounds.Y >= list.Y+list.Height {
			break
		}
		option := options[index]
		id := codeLanguageSuggestions.At(index)
		if index == code.selectedSuggestion || canvas.Hovered() == id || canvas.Focused() == id {
			canvas.Rounded(bounds, 5, palette.selected)
		}
		name := state.codeLanguageName(option)
		canvas.Label(ellipsize(canvas, name, bounds.Width-40, 12), bounds.Inset(gfx.Insets{Left: 9, Right: 28}), gfx.TextStyle{Size: 12, Color: palette.ink})
		if option == current {
			canvas.LabelCenter("✓", ui.Rect{X: bounds.X + bounds.Width - 26, Y: bounds.Y, Width: 21, Height: bounds.Height}, gfx.TextStyle{Size: 12, Color: palette.accent})
		}
		canvas.HitWithOptions(id, bounds, name, gfx.HitOptions{Role: gfx.RoleOption, Selected: option == current, SkipFocus: true})
	}
	canvas.Unclip()
	if extent > list.Height {
		thumb := max(14, list.Height*list.Height/extent)
		top := list.Y + (list.Height-thumb)*code.suggestionScroll/(extent-list.Height)
		canvas.Rounded(ui.Rect{X: list.X + list.Width - 4, Y: top, Width: 3, Height: thumb}, 1.5, palette.border)
	}
}

func paintCodeControl(canvas *gfx.Canvas, palette colors, id gfx.ID, bounds ui.Rect, value, label string, toggle, highlighted bool) {
	ink, role := palette.ink, gfx.RoleButton
	if toggle {
		role = gfx.RoleCheckbox
	}
	if highlighted {
		ink = palette.accent
	}
	if highlighted && toggle || canvas.Hovered() == id || canvas.Focused() == id {
		canvas.Rounded(bounds, 5, palette.selected)
	}
	canvas.LabelCenter(value, bounds, gfx.TextStyle{Size: 12, Color: ink})
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: role, Checked: toggle && highlighted, Hint: label})
}

func (state *model) onCodeLanguageKey(event gfx.KeyEvent) gfx.EventResult {
	if state.codeBlock.languageInput == nil || event.Release || event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape {
		return state.finishCodeLanguage(false)
	}
	if event.Target != codeLanguageInputID {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyEnter:
		return state.finishCodeLanguage(true)
	case gfx.KeyUp, gfx.KeyDown:
		code := &state.codeBlock
		if _, composing := code.languageInput.Composition(); composing {
			return gfx.EventResult{}
		}
		if options := state.fenceLanguageSuggestions(); len(options) > 0 {
			step := 1
			if event.Key == gfx.KeyUp {
				step = -1
			}
			code.selectedSuggestion = max(0, min(len(options)-1, code.selectedSuggestion+step))
			top := float32(code.selectedSuggestion * codeLanguageRowHeight)
			code.suggestionScroll = min(top, max(code.suggestionScroll, top+codeLanguageRowHeight-code.suggestionViewport.Height))
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: codeLanguageInputID}
	}
	return gfx.EventResult{}
}

func (state *model) onCodeLanguagePointer(event gfx.PointerEvent) gfx.EventResult {
	if state.codeBlock.languageInput == nil || event.Kind != gfx.PointerDown && event.Kind != gfx.PointerUp {
		return gfx.EventResult{}
	}
	if event.Target == codeLanguageInputID {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown {
		return gfx.EventResult{Handled: true, Focus: codeLanguageInputID}
	}
	if _, option := event.Target.In(codeLanguageSuggestions); option && event.Button == gfx.PointerButtonPrimary {
		return state.onCodeControl(event.Target)
	}
	if !contains(state.codeBlock.languagePopover, ui.Point{X: event.X, Y: event.Y}) {
		return state.finishCodeLanguage(false)
	}
	return gfx.EventResult{Handled: true, Focus: codeLanguageInputID}
}

func (state *model) scrollCodeLanguage(event gfx.ScrollEvent) gfx.EventResult {
	code := &state.codeBlock
	if code.languageInput == nil {
		return gfx.EventResult{}
	}
	if !contains(code.languagePopover, ui.Point{X: event.X, Y: event.Y}) {
		return gfx.EventResult{Handled: true}
	}
	maximum := max(0, float32(len(state.fenceLanguageSuggestions())*codeLanguageRowHeight)-code.suggestionViewport.Height)
	next := max(0, min(code.suggestionScroll+event.StepY(codeLanguageRowHeight), maximum))
	changed := next != code.suggestionScroll
	code.suggestionScroll = next
	return gfx.EventResult{Handled: true, Repaint: changed}
}

func (state *model) insertCodeNewline() {
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || !code.active || code.input == nil || code.source != current.Text() || code.documentID != current.ID ||
		current.Composing() || state.codeBlockComposing() {
		return
	}
	input := code.input
	value := input.String()
	position := byteOffsetOfRune(value, input.Caret())
	start := formatLineStart(value, position)
	line := value[start:formatLineEnd(value, start)]
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	prefix := strings.TrimRight(value[start:position], " \t")
	if prefix != "" && strings.ContainsRune("{[(:", rune(prefix[len(prefix)-1])) {
		unit := "\t"
		if strings.HasPrefix(indent, " ") && !strings.ContainsRune(indent, '\t') {
			unit = strings.Repeat(" ", min(len(indent), 4))
		}
		indent += unit
	}
	before := directedCodeSelection(input)
	input.Insert("\n" + indent)
	state.syncCodeBlockEdit(current, &before)
}

func (state *model) exitCodeBlockToParagraph() gfx.EventResult {
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || !code.active || code.source != current.Text() || code.documentID != current.ID {
		return gfx.EventResult{}
	}
	index := code.renderedIndex + 1
	blocks := markdownedit.Parse([]byte(code.source))
	mapping := renderedMapping(&state.parsed, blocks)
	if index < len(mapping) && mapping[index] >= 0 && blocks[mapping[index]].Editable {
		result := state.activateRendered(index)
		if state.rich.active {
			state.rich.input.MoveTo(0, false)
			state.historyReveal = true
		}
		return result
	}
	context := state.formatContext()
	return state.openParagraphAfter(context, context.end, index)
}

func (state *model) openParagraphAfter(context formatContext, end, index int) gfx.EventResult {
	newline := codeBoundaryNewline(context.source, end, context.document.Newline())
	position := end
	for count := 0; count < 2 && strings.HasPrefix(context.source[position:], newline); count++ {
		position += len(newline)
	}
	before := strings.Repeat(newline, 2-(position-end)/len(newline))
	at := position + len(before)
	after := formatBlockSuffix(context.source[position:], newline)
	if before+after != "" {
		state.commitFormat(context, position, position, before+after, at, at, markdownedit.Paragraph, -1)
	} else {
		state.openVirtualParagraph(index, at)
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.focusRequest}
}

func codeBoundaryNewline(source string, at int, fallback string) string {
	switch {
	case strings.HasPrefix(source[at:], "\r\n"):
		return "\r\n"
	case strings.HasPrefix(source[at:], "\n"):
		return "\n"
	default:
		return fallback
	}
}

func (state *model) revertEmptyCodeBlock() gfx.EventResult {
	return state.deleteCodeBlock(true)
}

func (state *model) deleteCodeBlock(requireEmpty bool) gfx.EventResult {
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || !code.active || code.input == nil || code.source != current.Text() || code.documentID != current.ID ||
		(requireEmpty && code.input.String() != "") || !state.pendingFormatCompositionFocus().IsZero() {
		return gfx.EventResult{}
	}
	context := state.formatContext()
	newline := codeBoundaryNewline(context.source, context.end, current.Newline())
	var before, after string
	if context.start > 0 && strings.TrimSpace(context.source[formatLineStart(context.source, context.start-1):context.start]) != "" {
		before = newline
	}
	if rest := context.source[context.end:]; rest != "" && !strings.HasPrefix(rest, newline+newline) {
		after = newline
	}
	at := context.start + len(before)
	state.commitFormat(context, context.start, context.end, before+after, at, at, markdownedit.Paragraph, -1)
	if n := len(state.rich.structureUndo); n > 0 {
		state.rich.structureUndo[n-1].noBackspaceUndo = true
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: state.focusRequest}
}

func (state *model) indentFencedCode(outdent bool) {
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || code.input == nil || code.documentID != current.ID || code.source != current.Text() ||
		code.block.Code.Visible != code.input.String() || current.Composing() || state.codeBlockComposing() {
		return
	}
	before := directedCodeSelection(code.input)
	if before.Start != before.End {
		state.indentSelectedCodeLines(outdent)
	} else if outdent {
		state.unindentCodeLine()
	} else {
		code.input.Insert("\t")
	}
	state.syncCodeBlockEdit(current, &before)
}

func directedCodeSelection(input *text.Editor) text.Range {
	selection := input.Selection()
	if input.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	return selection
}

func (state *model) unindentCodeLine() {
	input := state.codeBlock.input
	if input == nil || input.Selection().Start != input.Selection().End {
		return
	}
	value := input.String()
	caret := input.Caret()
	position := byteOffsetOfRune(value, caret)
	start := strings.LastIndexByte(value[:position], '\n') + 1
	indent := codeLineIndent(value, start)
	if indent == 0 {
		return
	}
	startRune := utf8.RuneCountInString(value[:start])
	input.SetSelection(text.Range{Start: startRune, End: startRune + indent})
	input.Insert("")
	input.MoveTo(max(startRune, caret-indent), false)
}

func codeLineIndent(value string, start int) int {
	if start < len(value) && value[start] == '\t' {
		return 1
	}
	indent := 0
	for start+indent < len(value) && indent < 4 && value[start+indent] == ' ' {
		indent++
	}
	return indent
}

func (state *model) indentSelectedCodeLines(backward bool) {
	input := state.codeBlock.input
	selection := input.Selection()
	if selection.Start == selection.End {
		return
	}
	value := input.String()
	start := byteOffsetOfRune(value, selection.Start)
	end := byteOffsetOfRune(value, selection.End)
	lineStart := strings.LastIndexByte(value[:start], '\n') + 1
	var lines []int
	for lineStart < end {
		lines = append(lines, lineStart)
		next := strings.IndexByte(value[lineStart:], '\n')
		if next < 0 {
			break
		}
		lineStart += next + 1
	}
	if len(lines) == 0 {
		return
	}
	var next strings.Builder
	next.Grow(len(value) + len(lines))
	cursor, startRemoved, endRemoved := 0, 0, 0
	for _, position := range lines {
		next.WriteString(value[cursor:position])
		if backward {
			removed := codeLineIndent(value, position)
			startRemoved += max(0, min(start-position, removed))
			endRemoved += max(0, min(end-position, removed))
			cursor = position + removed
		} else {
			next.WriteByte('\t')
			cursor = position
		}
	}
	next.WriteString(value[cursor:])
	if next.String() == value {
		return
	}
	newStart, newEnd := selection.Start+1, selection.End+len(lines)
	if backward {
		newStart, newEnd = selection.Start-startRemoved, selection.End-endRemoved
	}
	if input.Caret() == selection.Start {
		newStart, newEnd = newEnd, newStart
	}
	input.SetSelection(text.Range{Start: 0, End: input.RuneCount()})
	input.Insert(next.String())
	input.SetSelection(text.Range{Start: newStart, End: newEnd})
}

func (state *model) stepCodeBlockHistory(command app.TextCommand) app.TextState {
	code := &state.codeBlock
	current := state.session.Current()
	if current == nil || code.input == nil {
		return app.TextState{}
	}
	if code.documentID != current.ID || code.source != current.Text() {
		return app.TextStateOf(code.input)
	}
	if result, handled := state.stepInactiveRenderedHistory(command, code.input); handled {
		return result
	}
	before := current.Text()
	changed := false
	if command == app.TextUndo {
		changed = state.stepMarkdownEdit(current, true)
	} else {
		changed = state.stepMarkdownEdit(current, false)
	}
	if !changed {
		return app.TextStateOf(code.input)
	}
	if state.restoreOtherRenderedHistory(current, before, command) {
		state.table, state.codeBlock = tableEditor{}, codeBlockEditor{}
		return renderedHistoryInputState(state.rich.input)
	}
	delta := 1
	if command == app.TextUndo {
		delta = -1
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	if code.sourceIndex >= 0 && code.sourceIndex < len(blocks) && (codeBlockSourceChange(before, current.Text(), code.block, blocks[code.sourceIndex]) || fenceLanguageSourceChange(before, current.Text(), code.block, blocks[code.sourceIndex])) {
		state.moveInactiveRenderedHistory(current, before, delta, false)
		caret := min(code.input.Caret(), utf8.RuneCountInString(blocks[code.sourceIndex].Code.Visible))
		code.input = text.NewEditor(blocks[code.sourceIndex].Code.Visible)
		if selection, valid := codeSourceHistorySelection(current, blocks[code.sourceIndex]); valid {
			code.input.SetSelection(selection)
		} else {
			code.input.MoveTo(caret, false)
		}
		code.block, code.source, code.languageInput = blocks[code.sourceIndex], current.Text(), nil
		return app.TextStateOf(code.input)
	}
	if result, restored := state.restoreSpecialSourceHistory(current, before, command); restored {
		return result
	}
	state.moveInactiveRenderedHistory(current, before, delta, false)
	previousState := app.TextStateOf(code.input)
	state.specialHistorySourceFallback()
	return previousState
}

func (state *model) syncCodeBlock(current *session.Document) {
	state.syncCodeBlockEdit(current, nil)
}

func (state *model) syncCodeBlockEdit(current *session.Document, beforeSelection *text.Range) {
	code := &state.codeBlock
	if !code.active {
		return
	}
	if code.documentID != current.ID || code.source != current.Text() || code.input == nil {
		state.codeBlock = codeBlockEditor{}
		return
	}
	defer state.moveInactiveRenderedHistory(current, current.Text(), 1, true)
	if _, composing := code.input.Composition(); composing || code.input.String() == code.block.Code.Visible {
		return
	}
	value := code.input.String()
	next, err := markdownedit.ReplaceCode([]byte(code.source), code.block, value)
	if err != nil {
		if beforeSelection != nil {
			code.input = text.NewEditor(code.block.Code.Visible)
			code.input.SetSelection(*beforeSelection)
			return
		}
		replacement := strings.ReplaceAll(value, "\n", current.Newline())
		if value != "" && !code.block.Code.SingleLine {
			replacement += current.Newline()
		}
		if code.block.Code.SingleLine && strings.ContainsRune(value, '\n') {
			replacement = current.Newline() + replacement + current.Newline()
		}
		current.Editor.BreakUndoGroup()
		editErr := current.EditRange(code.block.Code.Start, code.block.Code.End, replacement)
		current.Editor.BreakUndoGroup()
		if editErr != nil {
			state.message = editErr.Error()
			return
		}
		state.codeBlockSourceFallback(current, code.block.Code.Start+len(replacement))
		return
	}
	body := code.block.Code
	newline := current.Newline()
	if beforeSelection != nil {
		newline = "\n"
		if body.Start >= 2 && code.source[body.Start-2] == '\r' {
			newline = "\r\n"
		}
		if value == "" && body.Start < body.End && !body.SingleLine {
			next = []byte(code.source[:body.Start] + newline + code.source[body.End:])
		}
	}
	end := len(next) - (len(code.source) - body.End)
	replacement := string(next[body.Start:end])
	previousBody := code.source[body.Start:body.End]
	var patchEnd int
	if beforeSelection == nil {
		patchEnd = len(strings.ReplaceAll(value[:byteOffsetOfRune(value, code.input.Caret())], "\n", newline))
	} else {
		patchEnd = fencedIndentPatchEnd(previousBody, replacement)
	}
	start, endBefore, inserted := renderedInputSourceEdit(previousBody, replacement, patchEnd)
	if beforeSelection == nil {
		current.Editor.BreakUndoGroup()
		err = current.EditRange(code.block.Code.Start+start, code.block.Code.Start+endBefore, inserted)
		current.Editor.BreakUndoGroup()
	} else {
		before := codeSelectionInSource(code.source, body.Start, body.Visible, *beforeSelection)
		after := codeSelectionInSource(string(next), body.Start, value, directedCodeSelection(code.input))
		patch := markdownedit.SourcePatch{Start: body.Start + start, End: body.Start + endBefore, Text: inserted}
		err = applyMarkdownPatch(current, patch, before, after)
		if err != nil {
			code.input = text.NewEditor(code.block.Code.Visible)
			code.input.SetSelection(*beforeSelection)
		}
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	if current.Text() != string(next) {
		state.codeBlockSourceFallback(current, code.block.Code.Start)
		return
	}
	blocks := markdownedit.Parse(next)
	if code.sourceIndex >= len(blocks) || !blocks[code.sourceIndex].Code.Editable || blocks[code.sourceIndex].Code.Visible != value {
		state.codeBlockSourceFallback(current, code.block.Code.Start)
		return
	}
	code.block, code.source = blocks[code.sourceIndex], current.Text()
	state.message = ""
}

func fencedIndentPatchEnd(before, after string) int {
	for len(before) > 0 && len(after) > 0 {
		previous, beforeWidth := utf8.DecodeLastRuneInString(before)
		current, afterWidth := utf8.DecodeLastRuneInString(after)
		if previous != current || beforeWidth != afterWidth {
			break
		}
		before = before[:len(before)-beforeWidth]
		after = after[:len(after)-afterWidth]
	}
	return len(after)
}

func (state *model) paintCodeBlock(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	appearance := state.markdownAppearance(palette)
	palette = appearance.palette
	code := &state.codeBlock
	value := code.input.String()
	selection := code.input.Selection()
	focused := canvas.Focused() == codeInputID
	snapshot := components.CodeEditorSnapshot{
		ID: codeInputID, Label: "围栏代码编辑器", Value: value,
		Focused: focused, ShowCaret: focused, Wrap: !state.preferences.DisableCodeWrap,
		LineNumbers: !state.preferences.HideCodeLineNumbers, CurrentLine: true, MatchBrackets: true,
		Placeholder: state.codePlaceholder(),
		FontSize:    state.readingSize() - 1, MonoFace: markdownCodeFace, Language: code.block.Code.Info,
		Caret:          byteOffsetOfRune(value, code.input.Caret()),
		SelectionStart: byteOffsetOfRune(value, selection.Start),
		SelectionEnd:   byteOffsetOfRune(value, selection.End),
	}
	if marked, composing := code.input.Composition(); composing {
		snapshot.CompositionStart = byteOffsetOfRune(value, marked.Start)
		snapshot.CompositionEnd = byteOffsetOfRune(value, marked.End)
	}
	var look components.MarkdownLook
	appearance.applyCodeLook(&look, state.dark)
	snapshot.Highlight = func(language, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
		dst = dst[:0]
		for _, run := range markdownCodeRuns(language, line, nil) {
			dst = append(dst, components.CodeEditorRun{Start: run.Start, End: run.End, Color: look.Code[run.Class]})
		}
		return dst
	}
	codeTheme := theme
	codeTheme.Foreground, codeTheme.CardForeground = palette.ink, palette.ink
	codeTheme.Background, codeTheme.Card = appearance.codeFill, appearance.codeFill
	codeTheme.Border, codeTheme.Ring, codeTheme.Primary, codeTheme.Input = appearance.codeFill, appearance.codeFill, appearance.codeFill, ui.Color{}
	composingConfirmation := state.confirmationCompositionFocus() == codeInputID
	if composingConfirmation {
		canvas.PushFocusScope(confirmScope)
	}
	canvas.Clip(bounds)
	code.view.Paint(canvas, codeTheme, bounds, snapshot)
	canvas.Unclip()
	if composingConfirmation {
		canvas.PopFocusScope()
	}
}

func (state *model) codePlaceholder() string {
	return state.local("输入代码…", "Type code…")
}
