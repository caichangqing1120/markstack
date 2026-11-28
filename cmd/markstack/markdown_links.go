package main

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	mdtext "github.com/yuin/goldmark/text"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type localMarkdownLink struct {
	start, closing, end int
	destination         string
}

type localMarkdownLinks struct {
	source string
	links  []localMarkdownLink
}

type localLinkParser struct{ parser.InlineParser }

var localLinkContext = parser.NewContextKey()

var markdownLinkParser = func() parser.Parser {
	inline := parser.DefaultInlineParsers()
	for index := range inline {
		if inline[index].Value == parser.NewLinkParser() {
			inline[index].Value = localLinkParser{parser.NewLinkParser()}
			break
		}
	}
	return goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParser(parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...), parser.WithInlineParsers(inline...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	))).Parser()
}()

func (wrapper localLinkParser) CloseBlock(parent ast.Node, reader mdtext.Reader, context parser.Context) {
	wrapper.InlineParser.(parser.CloseBlocker).CloseBlock(parent, reader, context)
}

func (wrapper localLinkParser) Parse(parent ast.Node, reader mdtext.Reader, context parser.Context) ast.Node {
	_, before := reader.Position()
	node := wrapper.InlineParser.Parse(parent, reader, context)
	if link, ok := node.(*ast.Link); ok && link.Reference == nil {
		_, after := reader.Position()
		destination := string(link.Destination)
		if _, ok := markdownLinkURL(destination); ok && after.Start > before.Start+2 {
			links := context.Get(localLinkContext).(*[]localMarkdownLink)
			*links = append(*links, localMarkdownLink{start: link.Pos(), closing: before.Start, end: after.Start, destination: destination})
		}
	}
	return node
}

func markdownLinkURL(destination string) (*url.URL, bool) {
	parsed, err := url.Parse(destination)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery {
		return nil, false
	}
	if parsed.Path == "" {
		return parsed, parsed.Fragment != ""
	}
	return parsed, workspace.IsMarkdown(parsed.Path)
}

func (links *localMarkdownLinks) update(source string) {
	if links.source == source {
		return
	}
	links.source = source
	links.links = links.links[:0]
	if !strings.Contains(source, "](") {
		return
	}
	context := parser.NewContext()
	context.Set(localLinkContext, &links.links)
	markdownLinkParser.Parse(mdtext.NewReader([]byte(source)), parser.WithContext(context))
}

func (links *localMarkdownLinks) at(offset int) string {
	index := sort.Search(len(links.links), func(index int) bool { return links.links[index].end > offset })
	if index < len(links.links) && offset >= links.links[index].start {
		return links.links[index].destination
	}
	return ""
}

func (links *localMarkdownLinks) project(document *markdown.Document) {
	if len(links.links) == 0 {
		return
	}
	marker := "https://markstack.invalid/0/"
	for strings.Contains(links.source, marker) {
		marker += "0"
	}
	var source strings.Builder
	position := 0
	for index, link := range links.links {
		if link.closing < position || link.end > len(links.source) || !strings.HasPrefix(links.source[link.closing:link.end], "](") || strings.ContainsAny(links.source[link.closing:link.end], "\r\n") {
			continue
		}
		source.WriteString(links.source[position : link.closing+2])
		source.WriteString(marker)
		source.WriteString(strconv.Itoa(index))
		source.WriteByte(')')
		position = link.end
	}
	if position == 0 {
		return
	}
	source.WriteString(links.source[position:])
	var projected markdown.Document
	projected.Parse(source.String())
	if len(projected.Blocks) != len(document.Blocks) {
		return
	}
	merge := func(original, changed []markdown.Span) {
		if len(original) != len(changed) {
			return
		}
		for index := range original {
			if original[index].Text != changed[index].Text || original[index].Style != changed[index].Style || original[index].Image != changed[index].Image {
				return
			}
		}
		for index, span := range changed {
			if token, ok := strings.CutPrefix(span.Link, marker); ok && original[index].Link == "" {
				if target, err := strconv.Atoi(token); err == nil && target >= 0 && target < len(links.links) {
					original[index].Link = links.links[target].destination
				}
			}
		}
	}
	for index := range document.Blocks {
		original, changed := &document.Blocks[index], &projected.Blocks[index]
		if original.Kind != changed.Kind {
			continue
		}
		if original.Kind == markdown.Table {
			cells, projectedCells := document.Cells(original), projected.Cells(changed)
			if len(cells) == len(projectedCells) {
				for index, cell := range cells {
					merge(document.CellSpans(cell), projected.CellSpans(projectedCells[index]))
				}
			}
		} else {
			merge(document.Spans(original), projected.Spans(changed))
		}
	}
}

func relativeMarkdownPath(root, path, link string) (string, error) {
	if root == "" || path == "" {
		return "", errors.New("请先打开包含当前文件的工作区")
	}
	if !portableMarkdownLinkPath(link) {
		return "", workspace.ErrOutsideWorkspace
	}
	from, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(from) {
		return "", workspace.ErrOutsideWorkspace
	}
	relative := filepath.Join(filepath.Dir(from), filepath.FromSlash(link))
	if !filepath.IsLocal(relative) {
		return "", workspace.ErrOutsideWorkspace
	}
	return relative, nil
}

func portableMarkdownLinkPath(link string) bool {
	if link == "" || strings.HasPrefix(link, "/") || strings.ContainsAny(link, `\:<>"|?*`) || strings.ContainsFunc(link, unicode.IsControl) {
		return false
	}
	for part := range strings.SplitSeq(link, "/") {
		if part != ".." && part != "." && strings.TrimRight(part, ". ") != part {
			return false
		}
	}
	return true
}

func (state *model) followMarkdownLink(destination string) gfx.EventResult {
	result := gfx.EventResult{Handled: true, Repaint: true}
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		result.Focus = focus
		return result
	}
	parsed, local := markdownLinkURL(destination)
	if !local {
		if markdownedit.SafeLinkURL(destination) {
			if err := app.OpenURL(destination); err != nil {
				state.message = err.Error()
			}
		}
		return result
	}
	current := state.session.Current()
	if parsed.Path != "" {
		relative, err := relativeMarkdownPath(state.session.Root, current.Path, parsed.Path)
		if err == nil && state.prepareDocumentNavigation() {
			var next *session.Document
			next, err = state.session.OpenWorkspaceMarkdown(relative)
			if err == nil && next != current {
				state.resetDocumentView()
				state.recordRecentFile(next.Path)
				current = next
			}
		}
		if err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				state.message = "找不到 Markdown 文件：" + parsed.Path
			case errors.Is(err, workspace.ErrOutsideWorkspace):
				state.message = "链接指向工作区外或不安全的路径：" + parsed.Path
			default:
				state.message = "无法打开链接：" + err.Error()
			}
			return result
		}
		if state.mode == editMode || state.mode == splitMode {
			result.Focus = sourceID
		}
	}
	state.message = ""
	if parsed.Fragment != "" {
		panel := &state.outlinePanel
		panel.index(current.ID, current.Editor.CommittedString())
		index, found := panel.anchors[strings.ToLower(parsed.Fragment)]
		if !found {
			index, found = panel.anchors[headingSlug(parsed.Fragment)]
		}
		if !found {
			state.message = "找不到标题：" + parsed.Fragment
			return result
		}
		entry := panel.entries[index]
		if state.mode == editMode || state.mode == splitMode {
			state.sourceAnchor = scrollMotion{}
			state.pendingSourceAnchor = entry.Rune
			result.Focus = sourceID
		}
		if state.mode != editMode {
			state.pendingAnchor = entry.start
			state.anchor = scrollMotion{}
			state.writing = writingMotion{}
		}
	}
	return result
}

func (state *model) paintSourceAnchor(canvas *gfx.Canvas, snapshotCaret int) bool {
	if state.pendingSourceAnchor < 0 {
		return false
	}
	state.writing.tracking = false
	position := state.stats.byteAt(state.pendingSourceAnchor)
	target, ok := state.source.YAtOffset(position)
	if !ok {
		return true
	}
	inset := float32(8)
	if state.preferences.TypewriterMode {
		inset = state.sourceRect.Height/2 - state.sourceSize()/2
	}
	target -= inset
	if !state.preferences.TypewriterMode {
		target = max(0, min(target, state.source.MaxScrollY()))
	}
	previous := state.sourceWritingScroll()
	state.setSourceWritingScroll(state.sourceAnchor.step(canvas, previous, target))
	if state.sourceWritingScroll() != previous {
		canvas.RequestFrame()
	}
	if !state.sourceAnchor.active {
		if current := state.session.Current(); !current.Composing() && snapshotCaret != position {
			current.Editor.MoveTo(state.pendingSourceAnchor, false)
			canvas.RequestFrame()
		}
		state.pendingSourceAnchor = -1
	}
	return true
}

func (state *model) previewAnchor(canvas *gfx.Canvas, bounds ui.Rect) {
	if state.pendingAnchor >= 0 {
		start := state.pendingAnchor
		state.pendingAnchor = -1
		index := sort.Search(len(state.parsed.Blocks), func(index int) bool { return state.parsed.Blocks[index].Start >= start })
		if index < len(state.preview.blocks) && state.parsed.Blocks[index].Start == start {
			if state.mode == renderedMode {
				result := state.activateRendered(index)
				state.focusRequest = result.Focus
				state.invalidator.Invalidate()
			}
			minimum, maximum := state.previewLimits(bounds)
			target := max(minimum, min(state.preview.blocks[index].top-8, maximum))
			state.previewScroll = state.anchor.step(canvas, state.previewScroll, target)
		}
	} else if state.anchor.active {
		state.previewScroll = state.anchor.step(canvas, state.previewScroll, state.anchor.target)
	}
}

func (state *model) markdownLinkAt(event gfx.PointerEvent) string {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind {
		return ""
	}
	point := ui.Point{X: event.X, Y: event.Y}
	primary := event.Modifiers&primaryModifier() != 0
	if primary && event.Target == sourceID && contains(state.sourceRect, point) {
		if offset, ok := state.source.OffsetAt(point); ok {
			state.localLinks.update(current.Text())
			return state.localLinks.at(offset)
		}
	}
	if !contains(state.previewRect, point) {
		return ""
	}
	_, previewHit := event.Target.In(previewLinks)
	_, rowHit := event.Target.In(renderedRows)
	_, cellHit := event.Target.In(renderedTableCells)
	if !previewHit && !rowHit && !cellHit && event.Target != richInputID {
		return ""
	}
	for _, hit := range state.previewLinkHits {
		if contains(hit.bounds, point) {
			if _, local := markdownLinkURL(hit.destination); primary || state.mode == readMode || !local && state.mode == splitMode {
				return hit.destination
			}
		}
	}
	if primary && event.Target == richInputID && state.rich.active && state.rich.index < len(state.parsed.Blocks) {
		if offset, ok := state.rich.offsetAt(point); ok {
			for _, span := range state.parsed.Spans(&state.parsed.Blocks[state.rich.index]) {
				if offset < len(span.Text) {
					return span.Link
				}
				offset -= len(span.Text)
			}
		}
	}
	return ""
}

type markdownLinkHit struct {
	bounds      ui.Rect
	destination string
}

func (state *model) onMarkdownLinkPointer(event gfx.PointerEvent) gfx.EventResult {
	switch event.Kind {
	case gfx.PointerDown:
		state.linkPress = state.markdownLinkAt(event)
		return gfx.EventResult{Handled: state.linkPress != ""}
	case gfx.PointerUp:
		pressed := state.linkPress
		state.linkPress = ""
		if pressed != "" {
			if pressed == state.markdownLinkAt(event) {
				return state.followMarkdownLink(pressed)
			}
			return gfx.EventResult{Handled: true}
		}
	}
	return gfx.EventResult{}
}

func (state *model) captureMarkdownLinkHits(canvas *gfx.Canvas, first int) {
	state.previewLinkHits = state.previewLinkHits[:0]
	for _, hit := range canvas.Hits()[first:] {
		index, ok := hit.ID.In(previewLinks)
		if !ok || index >= state.preview.LinkCount() {
			continue
		}
		destination := state.preview.Link(index)
		state.previewLinkHits = append(state.previewLinkHits, markdownLinkHit{bounds: hit.Bounds, destination: destination})
		if state.mode == readMode && canvas.Clicked(hit.ID) {
			canvas.AfterPaint(func() { state.followMarkdownLink(destination); state.invalidator.Invalidate() })
		}
	}
}
