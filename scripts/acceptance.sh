#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
artifacts="$root/build/acceptance/latest"
mkdir -p "$artifacts"
work="$(mktemp -d /tmp/markstack-acceptance.XXXXXX)"
trap 'rm -rf "$work"' EXIT
source "$root/scripts/acceptance-state.sh"

go version > "$artifacts/environment.txt"
acceptance_run go test -race ./... -count=1 -v | tee "$artifacts/tests.log"
go vet ./...
formatting="$(gofmt -l cmd/markstack internal)"
if [[ -n "$formatting" ]]; then
    printf 'Unformatted Go files:\n%s\n' "$formatting" >&2
    exit 1
fi
go build -tags vivahead -o build/markstack-headless ./cmd/markstack
export_artifacts="$work/html-export"
mkdir -p "$export_artifacts"
MARKSTACK_EXPORT_ARTIFACT_DIR="$export_artifacts" acceptance_run go test -tags vivahead ./cmd/markstack -run '^TestHTMLExportArtifacts$' -count=1 -v | tee "$artifacts/export-tests.log"
mkdir -p "$artifacts/html-export"
cp "$export_artifacts"/* "$artifacts/html-export/"
pdf_artifacts="$root/build/acceptance/pdf"
mkdir -p "$pdf_artifacts"
MARKSTACK_PDF_ARTIFACT_DIR="$pdf_artifacts" acceptance_run go test ./internal/exportpdf -run '^TestRender' -count=1 -v | tee "$artifacts/pdf-tests.log"
if command -v pdftoppm >/dev/null 2>&1; then
    pdftoppm -f 1 -l 1 -r 120 -png -singlefile "$pdf_artifacts/Markdown.pdf" "$pdf_artifacts/Markdown-first"
    pdftoppm -f 1 -l 1 -r 120 -png -singlefile "$pdf_artifacts/Rich.pdf" "$pdf_artifacts/Rich-page"
    pdftoppm -f 1 -l 1 -r 120 -png -singlefile "$pdf_artifacts/Table-Image.pdf" "$pdf_artifacts/Table-Image-page"
    pdftoppm -f 1 -l 1 -r 120 -png -singlefile "$pdf_artifacts/Table-Cell-Image.pdf" "$pdf_artifacts/Table-Cell-Image-page"
    pdftoppm -f 1 -l 1 -r 120 -png -singlefile "$pdf_artifacts/Mixed-Long.pdf" "$pdf_artifacts/Mixed-Long-page"
    pdftoppm -f 1 -l 1 -r 120 -png -singlefile "$pdf_artifacts/Welcome-renderer.pdf" "$pdf_artifacts/Welcome-renderer-page"
fi
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee "$artifacts/pdf-menu-protocol.log"
resize 740 520
click "Welcome.md"
click "文件"
shot "$artifacts/pdf-menu.png"
layout
quit
EOF
initial_fixture="$work/initial-workspace"
mkdir -p "$initial_fixture"
cp examples/Welcome.md "$initial_fixture/Welcome.md"
cp examples/Rendered-Enter.md "$initial_fixture/Rendered-Enter.md"
rendered_fixture="$root/build/acceptance/rendered-fixture"
mkdir -p "$rendered_fixture"
cp examples/Rendered-Enter.md "$rendered_fixture/Rendered-Enter.md"
split_fixture="$root/build/acceptance/split-fixture"
mkdir -p "$split_fixture"
cp examples/Rendered-Split.md "$split_fixture/Rendered-Split.md"
merge_fixture="$root/build/acceptance/merge-fixture"
mkdir -p "$merge_fixture"
cp examples/Rendered-Enter.md "$merge_fixture/Rendered-Enter.md"
heading_fixture="$root/build/acceptance/heading-fixture"
mkdir -p "$heading_fixture"
cp examples/Rendered-Enter.md "$heading_fixture/Rendered-Enter.md"
inline_fixture="$root/build/acceptance/inline-fixture"
mkdir -p "$inline_fixture"
cp examples/Rendered-Inline.md "$inline_fixture/Rendered-Inline.md"
bold_fixture="$root/build/acceptance/bold-fixture"
mkdir -p "$bold_fixture"
cp examples/Rendered-Enter.md "$bold_fixture/Rendered-Enter.md"
italic_fixture="$root/build/acceptance/italic-fixture"
mkdir -p "$italic_fixture"
cp examples/Rendered-Enter.md "$italic_fixture/Rendered-Enter.md"
link_fixture="$root/build/acceptance/link-fixture"
mkdir -p "$link_fixture"
cp examples/Rendered-Enter.md "$link_fixture/Rendered-Enter.md"
list_fixture="$root/build/acceptance/list-fixture"
mkdir -p "$list_fixture"
cp examples/Rendered-List.md "$list_fixture/Rendered-List.md"
list_exit_fixture="$root/build/acceptance/list-exit-fixture"
mkdir -p "$list_exit_fixture"
cp examples/Rendered-List.md "$list_exit_fixture/Rendered-List.md"
blank_fixture="$root/build/acceptance/blank-fixture"
mkdir -p "$blank_fixture"
cp examples/Rendered-Enter.md "$blank_fixture/Rendered-Enter.md"
ordered_fixture="$root/build/acceptance/ordered-fixture"
mkdir -p "$ordered_fixture"
cp examples/Rendered-Ordered.md "$ordered_fixture/Rendered-Ordered.md"
typora_fixture="$root/build/acceptance/typora-fixture"
mkdir -p "$typora_fixture"
cp examples/Welcome.md "$typora_fixture/Welcome.md"
long_fixture="$root/build/acceptance/long-fixture"
mkdir -p "$long_fixture"
cp examples/Rendered-Long.md "$long_fixture/Rendered-Long.md"
mixed_fixture="$root/build/acceptance/mixed-fixture"
mkdir -p "$mixed_fixture"
cp examples/Rendered-Mixed.md "$mixed_fixture/Rendered-Mixed.md"
mixed_wrapped_fixture="$root/build/acceptance/mixed-wrapped-fixture"
mkdir -p "$mixed_wrapped_fixture"
cp examples/Rendered-Mixed-Long.md "$mixed_wrapped_fixture/Rendered-Mixed-Long.md"
selection_fixture="$root/build/acceptance/selection-fixture"
mkdir -p "$selection_fixture"
cp examples/Rendered-Split.md "$selection_fixture/Rendered-Split.md"
italic_selection_fixture="$root/build/acceptance/italic-selection-fixture"
mkdir -p "$italic_selection_fixture"
cp examples/Rendered-Mixed.md "$italic_selection_fixture/Rendered-Mixed.md"
link_selection_fixture="$root/build/acceptance/link-selection-fixture"
mkdir -p "$link_selection_fixture"
cp examples/Rendered-Mixed.md "$link_selection_fixture/Rendered-Mixed.md"
strike_fixture="$root/build/acceptance/strike-fixture"
mkdir -p "$strike_fixture"
cp examples/Rendered-Split.md "$strike_fixture/Rendered-Split.md"
block_fixture="$root/build/acceptance/block-fixture"
mkdir -p "$block_fixture"
cp examples/Rendered-Enter.md "$block_fixture/Rendered-Enter.md"
task_fixture="$root/build/acceptance/task-fixture"
mkdir -p "$task_fixture"
cp examples/Rendered-Enter.md "$task_fixture/Rendered-Enter.md"
code_fixture="$root/build/acceptance/code-fixture"
mkdir -p "$code_fixture"
cp examples/Rendered-Enter.md "$code_fixture/Rendered-Enter.md"
fenced_fixture="$root/build/acceptance/fenced-code-fixture"
mkdir -p "$fenced_fixture"
cp cmd/markstack/testdata/Fenced-Code.md "$fenced_fixture/Fenced-Code.md"
fenced_crlf_fixture="$root/build/acceptance/fenced-code-crlf-fixture"
mkdir -p "$fenced_crlf_fixture"
printf '\357\273\277# Code\r\n\r\n```go\r\none\r\ntwo\r\n```\r\n\r\nTail\r\n' > "$fenced_crlf_fixture/Code.md"
wrapped_selection_fixture="$root/build/acceptance/wrapped-selection-fixture"
heading_metrics_fixture="$root/build/acceptance/heading-metrics-fixture"
mkdir -p "$heading_metrics_fixture"
cp cmd/markstack/testdata/Rendered-Heading.md "$heading_metrics_fixture/Heading.md"
mkdir -p "$wrapped_selection_fixture"
cp cmd/markstack/testdata/Rendered-Wrapped-Selection.md "$wrapped_selection_fixture/Wrapped-Selection.md"
table_fixture="$root/build/acceptance/table-fixture"
mkdir -p "$table_fixture"
cp examples/Rendered-Enter.md "$table_fixture/Rendered-Enter.md"
tree_fixture="$work/file-tree"
mkdir -p "$tree_fixture"
cp examples/Rendered-Enter.md "$tree_fixture/Sample.md"
drag_fixture="$work/drag"
drag_workspace="$work/dropped-workspace"
recent_store="$work/recent-private/recent.json"
template_fixture="$work/template-workspace"
rich_search_fixture="$work/rich-search"
real_rich_fixture="$root/build/acceptance/real-mkdown-fixture"
real_structure_fixture="$root/build/acceptance/real-mkdown-structure-fixture"
rich_cross_fixture="$work/rich-cross"
rich_new_fixture="$work/rich-new"
rich_paragraph_fixture="$work/rich-paragraph"
rich_structure_fixture="$work/rich-structure"
rich_styled_fixture="$work/rich-styled"
rich_boundary_fixture="$work/rich-boundary"
rich_merge_fixture="$work/rich-styled-merge"
rich_list_fixture="$work/rich-list"
rich_table_fixture="$work/rich-table"
rich_exit_fixture="$work/rich-list-exit"
rich_middle_fixture="$work/rich-list-middle"
rich_exit_roundtrip_fixture="$work/rich-list-roundtrip"
source_code_fixture="$work/code"
media_fixture="$work/media"
settings_file="$work/settings-private/settings.json"
mkdir -p "$drag_fixture" "$drag_workspace" "$template_fixture" "$rich_search_fixture" "$real_rich_fixture" "$real_structure_fixture" "$rich_cross_fixture" "$rich_new_fixture" "$rich_paragraph_fixture" "$rich_structure_fixture" "$rich_styled_fixture" "$rich_boundary_fixture" "$rich_merge_fixture" "$rich_list_fixture" "$rich_table_fixture" "$rich_exit_fixture" "$rich_middle_fixture" "$rich_exit_roundtrip_fixture" "$source_code_fixture" "$media_fixture/assets"
cp examples/Welcome.md "$drag_fixture/drag-valid.md"
cp examples/Rendered-Enter.md "$drag_fixture/drag-other.md"
cp examples/Welcome.md "$drag_fixture/unsupported.xlsx"
cp examples/Rendered-Enter.md "$drag_workspace/in-folder.md"
cp examples/Rendered-Split.md "$template_fixture/existing.md"
cp internal/richedit/testdata/Rich-HTML.mh "$rich_search_fixture/Rich-HTML.mh"
cp internal/richedit/testdata/Real-MkDown.mh "$real_rich_fixture/Real-MkDown.mh"
cp internal/richedit/testdata/Real-MkDown-Structure.mh "$real_structure_fixture/Real-MkDown-Structure.mh"
cp internal/richedit/testdata/Rich-HTML.mh "$rich_cross_fixture/Rich-HTML.mh"
cp internal/richedit/testdata/Paragraphs.mh "$rich_paragraph_fixture/Paragraphs.mh"
cp internal/richedit/testdata/Structure.mh "$rich_structure_fixture/Structure.mh"
printf '<p>before <strong>bold</strong> after</p>\n<!--keep-->\n' > "$rich_styled_fixture/Styled.mh"
printf '<p>before <strong>bold</strong> after</p>\n<!--keep-->\n' > "$rich_boundary_fixture/Boundary.mh"
printf '<p>before <strong>bo</strong></p>\n<p><strong>ld</strong> after</p>\n<!--keep-->\n' > "$rich_merge_fixture/Merge.mh"
printf '<ol start="9"><li>AlphaBeta</li><li>Tail</li></ol>\n<!--keep-->\n' > "$rich_list_fixture/List.mh"
printf '<table><tbody><tr><td>A</td><td>B</td></tr></tbody></table>\n<!--keep-->\n' > "$rich_table_fixture/Table.mh"
printf '<ol start="9"><li>First</li><li><br></li></ol>\n<!--keep-->\n' > "$rich_exit_fixture/Exit.mh"
printf '<ol start="9"><li>one</li><li><br></li><li>three</li></ol>\n<!--keep-->\n' > "$rich_middle_fixture/Middle.mh"
printf '<ol start="9"><li>one</li><li><br></li><li>three</li></ol>\n<!--keep-->\n' > "$rich_exit_roundtrip_fixture/Roundtrip.mh"
cp cmd/markstack/testdata/code/main.go cmd/markstack/testdata/code/literal.txt cmd/markstack/testdata/code/page.html "$source_code_fixture/"
printf '# 图片示例\n\n![本地图](assets/p.png)\n\n正文仍可阅读。\n' > "$media_fixture/Media.md"
printf '# 混排图片\n\n前文 ![彩条](assets/p.png) 中间 ![青图](assets/q.png) 后文\n\n远程 ![不可用](https://example.org/p.png) 仍可读。\n\n丢失 ![缺失](assets/missing.png) 仍可读。\n' > "$media_fixture/Mixed.md"
ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc2=size=320x180:rate=1 -frames:v 1 -update 1 -y "$media_fixture/assets/p.png"
ffmpeg -hide_banner -loglevel error -f lavfi -i color=c=0x5caa7d:size=160x100:rate=1 -frames:v 1 -update 1 -y "$media_fixture/assets/q.png"
cp "$media_fixture/Media.md" "$work/media-original.md"
cp "$media_fixture/Mixed.md" "$work/mixed-original.md"
cp "$media_fixture/assets/p.png" "$work/media-original.png"
cp "$media_fixture/assets/q.png" "$work/media-q-original.png"

VIVA_HEADLESS=1 acceptance_run ./build/markstack-headless "$initial_fixture" <<EOF | tee "$artifacts/protocol.log"
windows
shot "$artifacts/initial.png"
click "Welcome.md"
click "阅读"
layout
shot "$artifacts/read.png"
click "分栏"
layout
shot "$artifacts/split.png"
click "文件"
shot "$artifacts/file-menu.png"
layout
key esc
click "明暗"
layout
shot "$artifacts/dark.png"
click "阅读"
layout
shot "$artifacts/dark-read.png"
click "分栏"
click "明暗"
resize 740 520
layout
shot "$artifacts/narrow.png"
resize 1200 780
key cmd+n
type # Acceptance draft
key enter
type 中文验收
savefile cancel
key cmd+s
editor
savefile "$work/draft.md"
key cmd+s
savefile "$work/copy.md"
key cmd+shift+s
click "新建"
type Keep me
click "关闭 未命名-4.md"
shot "$artifacts/unsaved.png"
key cmd+n
click "取消"
click "Markdown 源码编辑器"
editor
click "关闭 未命名-4.md"
click "不保存"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$initial_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1605 955
click "Welcome.md"
click "阅读"
layout
shot "$artifacts/typora-size.png"
click "排版"
click "编辑 墨栈"
type 笔记
editor
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1605 955
click "Welcome.md"
click "排版"
shot "$artifacts/rendered-idle.png"
click "编辑 墨栈"
type 笔记
shot "$artifacts/rendered-active.png"
key shift+down
shot "$artifacts/rendered-selection.png"
key cmd+c
clip
key cmd+x
key cmd+z
click "排版"
click "明暗"
shot "$artifacts/rendered-dark.png"
resize 740 520
layout
shot "$artifacts/rendered-narrow.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$wrapped_selection_fixture" <<EOF | tee "$artifacts/wrapped-selection-protocol.log"
resize 740 520
click "Wrapped-Selection.md"
click "排版"
click "编辑 Title"
down 330 134
move 278 211
up 278 211
shot "$artifacts/rendered-wrapped-cross-selection-740.png"
key cmd+c
clip
layout
clip 续
key cmd+v
editor
type !
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
editor
type ?
editor
key cmd+z
key cmd+z
key cmd+z
key cmd+s
quit
EOF
if ! rg -Fq 'ok "\n加粗 多行段落需要按视觉位置选择文字。多行段落需要按视觉位置选择文字。"' "$artifacts/wrapped-selection-protocol.log" ||
    ! rg -Fq 'ok value="Title续!多行段落需要按视觉位置选择文字。' "$artifacts/wrapped-selection-protocol.log" ||
    ! rg -Fq 'ok value="Title续!?多行段落需要按视觉位置选择文字。' "$artifacts/wrapped-selection-protocol.log" ||
    ! rg -Fq 'selection=8,8 secure=false' "$artifacts/wrapped-selection-protocol.log" ||
    ! rg -q '^ok 0 problems$' "$artifacts/wrapped-selection-protocol.log" ||
    ! cmp -s "$wrapped_selection_fixture/Wrapped-Selection.md" cmd/markstack/testdata/Rendered-Wrapped-Selection.md ||
    ! node "$root/scripts/check-ui-screenshot.cjs" "$artifacts/rendered-wrapped-cross-selection-740.png" 740 520; then
    printf 'Wrapped cross-block selection lost visible text, layout, source bytes or its current screenshot.\n' >&2
    exit 1
fi

MARKSTACK_REQUIRE_NATIVE_COLUMN=1 acceptance_run go test -tags vivahead ./cmd/markstack -run '^TestMarkdownReferenceColumn(NativeHeadingBounds|WelcomeNativeBounds)$' -count=1 -v | tee "$artifacts/heading-native-bounds.log"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$heading_metrics_fixture" <<EOF | tee "$artifacts/heading-metrics-protocol.log"
resize 740 520
click "Heading.md"
click "排版"
shot "$artifacts/rendered-heading-reader-740.png"
click "id:markstack/rendered-rows/#0"
click 232 204
type X
editor
shot "$artifacts/rendered-heading-active-740.png"
layout
key cmd+s
key cmd+z
key cmd+s
compose 中文预编辑中文预编辑中文预编辑中文预编辑
shot "$artifacts/rendered-heading-preedit-740.png"
layout
key cmd+s
expect text "finish text composition before saving" within 1000
type 验
editor
shot "$artifacts/rendered-heading-committed-740.png"
layout
key cmd+z
key cmd+s
quit
EOF
if ! rg -Fq '东西X南北' "$artifacts/heading-metrics-protocol.log" ||
    ! rg -Fq '东西验南北' "$artifacts/heading-metrics-protocol.log" ||
    ! rg -Fq 'selection=29,29 secure=false' "$artifacts/heading-metrics-protocol.log" ||
    ! cmp -s "$heading_metrics_fixture/Heading.md" cmd/markstack/testdata/Rendered-Heading.md; then
    printf 'Wrapped heading changed its click target, composition layout or source bytes.\n' >&2
    exit 1
fi
if ! awk '
/^(offscreen|clipped|spill|overlap|collide) / {
    if ($1 != "spill" || $2 != "x=220" || $3 !~ /^y=(110|153|196|239)$/ ||
        $4 !~ /^w=[0-9.]+$/ || substr($4, 3)+0 <= 490 || substr($4, 3)+0 > 504.01 || $5 != "h=43" ||
        $0 !~ /reaches out of "渲染块编辑器" at x=220 y=110 w=490 h=(178|221)$/) bad=1
}
END { exit bad }
' "$artifacts/heading-metrics-protocol.log"; then
    printf 'Wrapped heading has an unexplained layout diagnostic; native glyph bounds are checked separately.\n' >&2
    exit 1
fi
for state in reader active preedit committed; do
    if ! node "$root/scripts/check-ui-screenshot.cjs" "$artifacts/rendered-heading-$state-740.png" 740 520; then
        printf 'Heading %s current screenshot is invalid.\n' "$state" >&2
        exit 1
    fi
done

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$fenced_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Fenced-Code.md"
click "排版"
click "源码编辑 Markdown 块"
layout
shot "$artifacts/rendered-code-active-740.png"
key cmd+a
layout
shot "$artifacts/rendered-code-selected-740.png"
key tab
layout
shot "$artifacts/rendered-code-indented-740.png"
key shift+tab
key right
type 新
key enter
type 尾
layout
shot "$artifacts/rendered-code-multiline-740.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$fenced_crlf_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Code.md"
click "排版"
click "源码编辑 Markdown 块"
compose ni
key cmd+s
quit
EOF
if ! cmp -s "$fenced_crlf_fixture/Code.md" <(printf '\357\273\277# Code\r\n\r\n```go\r\none\r\ntwo\r\n```\r\n\r\nTail\r\n'); then
    printf 'Fenced-code preedit reached disk before confirmation.\n' >&2
    exit 1
fi
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$fenced_crlf_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Code.md"
click "排版"
click "源码编辑 Markdown 块"
compose ni
type 你
key enter
type 新
layout
shot "$artifacts/rendered-code-crlf-740.png"
key cmd+s
quit
EOF
if ! cmp -s "$fenced_crlf_fixture/Code.md" <(printf '\357\273\277# Code\r\n\r\n```go\r\none\r\ntwo你\r\n新\r\n```\r\n\r\nTail\r\n'); then
    printf 'Fenced-code Save changed BOM, CRLF, fences or neighboring source.\n' >&2
    exit 1
fi

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1605 729
click "Welcome.md"
click "排版"
shot "$artifacts/typora-height.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$typora_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1605 729
click "Welcome.md"
click "排版"
shot "$artifacts/typora-matched-markstack.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Welcome.md"
click "分栏"
shot "$artifacts/source-grid-split.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Welcome.md"
click "文内查找"
type Markdown
shot "$artifacts/find-document.png"
key enter
shot "$artifacts/find-next.png"
key esc
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Welcome.md"
click "排版"
click "文内查找"
type Markdown
shot "$artifacts/rendered-find.png"
key enter
shot "$artifacts/rendered-find-next.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Welcome.md"
click "排版"
click "编辑 墨栈"
click "标题级别"
layout
shot "$artifacts/heading-menu.png"
click "H2"
shot "$artifacts/heading-h2.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Welcome.md"
click "排版"
click "大纲"
click "Markdown 示例"
shot "$artifacts/rendered-outline.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rendered_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
key enter
shot "$artifacts/rendered-empty-paragraph.png"
compose 中文预编辑中文预编辑中文预编辑中文预编辑
expect text "Tail" within 1000
shot "$artifacts/rendered-empty-preedit.png"
layout
key cmd+s
expect text "finish text composition before saving" within 1000
type Next
expect no-text "finish text composition before saving" within 1000
shot "$artifacts/rendered-new-paragraph.png"
key enter
type Third
shot "$artifacts/rendered-third-paragraph.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$split_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Split.md"
click "排版"
click "编辑 BodyText"
key left
key left
key left
key left
key enter
shot "$artifacts/rendered-split-paragraph.png"
type New
shot "$artifacts/rendered-split-input.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$merge_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Enter.md"
click "排版"
click "编辑 Tail"
key left
key left
key left
key left
key backspace
shot "$artifacts/rendered-merged-paragraph.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$heading_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Enter.md"
click "排版"
click "编辑 Title"
key enter
type Intro
shot "$artifacts/rendered-heading-new-paragraph.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$inline_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Inline.md"
click "排版"
click "编辑 bold"
type X
shot "$artifacts/rendered-bold-active.png"
click "编辑 label"
type Y
shot "$artifacts/rendered-link-active.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$bold_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "加粗整段"
type X
shot "$artifacts/rendered-bold-command.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$italic_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "斜体整段"
type X
shot "$artifacts/rendered-italic-command.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$link_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "链接整段"
shot "$artifacts/rendered-link-input-narrow.png"
layout
type https://example.com/doc
key enter
type X
shot "$artifacts/rendered-link-command.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$list_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-List.md"
click "排版"
click "编辑 two"
key enter
shot "$artifacts/rendered-empty-list-item.png"
type new
shot "$artifacts/rendered-new-list-item.png"
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$list_exit_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-List.md"
click "排版"
click "编辑 two"
key enter
key enter
shot "$artifacts/rendered-list-exit.png"
type Body
shot "$artifacts/rendered-list-paragraph.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$blank_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
key enter
key enter
shot "$artifacts/rendered-second-empty-paragraph.png"
type Next
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$ordered_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Ordered.md"
click "排版"
click "编辑 two"
key enter
shot "$artifacts/rendered-ordered-empty.png"
type three
shot "$artifacts/rendered-ordered-input.png"
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$long_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Long.md"
click "排版"
click 330 190
shot "$artifacts/rendered-long-active.png"
layout
click 270 218
type 新
shot "$artifacts/rendered-long-second-row.png"
layout
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$mixed_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Mixed.md"
click "排版"
click "编辑 before bold after"
shot "$artifacts/rendered-mixed-active.png"
type X
shot "$artifacts/rendered-mixed-edited.png"
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$mixed_wrapped_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Mixed-Long.md"
click "排版"
shot "$artifacts/rendered-mixed-wrapped-idle.png"
click 330 190
shot "$artifacts/rendered-mixed-wrapped-active.png"
key shift+down
shot "$artifacts/rendered-mixed-wrapped-selection.png"
type 新
shot "$artifacts/rendered-mixed-wrapped-edited.png"
key cmd+z
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$tree_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "新建 Markdown 文件"
shot "$artifacts/file-tree-name-740.png"
type draft.md
key enter
settle 100
type # Native draft
key cmd+s
click "重命名选中项"
key cmd+a
type renamed.md
key enter
settle 100
shot "$artifacts/file-tree-renamed-740.png"
resize 1200 780
layout
shot "$artifacts/file-tree-renamed-1200.png"
resize 740 520
click "Sample.md"
click "移到系统回收站"
shot "$artifacts/file-tree-trash-confirm-740.png"
layout
key esc
click "明暗"
click "移到系统回收站"
shot "$artifacts/file-tree-trash-dark-740.png"
layout
key esc
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Welcome.md"
down 202 220
move 360 220
up 360 220
click "大纲"
down 1016 220
move 840 220
up 840 220
layout
shot "$artifacts/panes-resized-1200.png"
resize 740 520
layout
shot "$artifacts/panes-resized-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
dragover 500 200
drop 500 200 "$drag_fixture/drag-valid.md"
editor
shot "$artifacts/drag-open-file.png"
drop 500 200 "$drag_fixture/drag-other.md" "$drag_fixture/unsupported.xlsx"
click "Markdown 源码编辑器"
type X
drop 500 200 "$drag_workspace"
shot "$artifacts/drag-workspace-confirm.png"
key esc
editor
drop 500 200 "$drag_workspace"
click "不保存"
waitfor "in-folder.md" 5000
layout
shot "$artifacts/drag-open-workspace.png"
quit
EOF

MARKSTACK_RECENT_PATH="$recent_store" VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
drop 500 200 "$drag_fixture/drag-valid.md"
quit
EOF

MARKSTACK_RECENT_PATH="$recent_store" VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "文件"
click "最近打开"
layout
shot "$artifacts/recent-workspaces-740.png"
click "最近文件"
waitfor "drag-valid.md" 5000
layout
shot "$artifacts/recent-files-740.png"
click "drag-valid.md"
settle 100
click "源码"
settle 100
click "Markdown 源码编辑器"
editor
click "文件"
click "最近打开"
click "examples"
layout
shot "$artifacts/recent-reopened-workspace.png"
quit
EOF

session_fixture="$root/build/acceptance/session-fixture"
mkdir -p "$session_fixture"
cp examples/Rendered-Enter.md "$session_fixture/first.md"
cp examples/Rendered-Enter.md "$session_fixture/second.md"
session_private="$work/session-private"
mkdir -m 0700 "$session_private"
MARKSTACK_SESSION_PATH="$session_private/session.json" MARKSTACK_RECOVERY_PATH="$session_private/recovery.json" VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$session_fixture" <<EOF | tee "$artifacts/session-restore-protocol.log"
resize 740 520
click "first.md"
click "second.md"
click "Markdown 源码编辑器"
key cmd+a
key left
key right
editor
quit
EOF

MARKSTACK_SESSION_PATH="$session_private/session.json" MARKSTACK_RECOVERY_PATH="$session_private/recovery.json" VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless <<EOF | tee -a "$artifacts/session-restore-protocol.log"
resize 740 520
editor
layout
shot "$artifacts/session-restored-740.png"
quit
EOF
if [[ "$(rg -c 'selection=1,1 secure=false' "$artifacts/session-restore-protocol.log" || true)" != 2 ]] ||
    ! cmp -s "$session_fixture/first.md" examples/Rendered-Enter.md ||
    ! cmp -s "$session_fixture/second.md" examples/Rendered-Enter.md ||
    [[ -e "$session_private/recovery.json" ]]; then
    printf 'Clean session restore changed source, caret or dirty recovery journal.\n' >&2
    exit 1
fi

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$template_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "文件"
shot "$artifacts/template-menu-740.png"
layout
click "笔记模板"
type 项目笔记
editor
shot "$artifacts/template-note-740.png"
savefile cancel
click "文件"
click "另存为"
savefile "$template_fixture/existing.md"
click "文件"
click "另存为"
savefile "$template_fixture/note.md"
click "文件"
click "另存为"
click "文件"
click "方案模板"
shot "$artifacts/template-proposal-740.png"
click "关闭 方案-3.md"
shot "$artifacts/template-unsaved-confirm-740.png"
key esc
savefile "$template_fixture/proposal.md"
click "文件"
click "另存为"
layout
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_search_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
key cmd+shift+f
type 斜体
key enter
waitfor "A & bold 斜体 link" 5000
layout
shot "$artifacts/rich-html-search-results-740.png"
click "A & bold 斜体 link"
settle 100
layout
shot "$artifacts/rich-html-search-open-740.png"
click "阅读"
layout
shot "$artifacts/rich-html-search-read-740.png"
click "源码"
layout
shot "$artifacts/rich-html-search-source-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$real_rich_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Real-MkDown.mh"
layout
shot "$artifacts/real-mkdown-rich-740.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$real_structure_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Real-MkDown-Structure.mh"
layout
shot "$artifacts/real-mkdown-structure-740.png"
click "编辑 HTML 文本 值 & 保留"
key tab
type 新
layout
shot "$artifacts/real-mkdown-structure-new-row-740.png"
key cmd+z
key cmd+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_cross_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rich-HTML.mh"
click "编辑 HTML 文本 A &"
down 265 121
move 301 121
up 301 121
layout
shot "$artifacts/rich-html-cross-selected-1200.png"
type 新
layout
shot "$artifacts/rich-html-cross-edited-1200.png"
key cmd+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_new_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "文件"
shot "$artifacts/rich-html-new-menu-740.png"
layout
click "新建富文本"
type 开始
layout
shot "$artifacts/rich-html-new-active-740.png"
savefile "$rich_new_fixture/draft.mh"
key cmd+s
click "阅读"
layout
shot "$artifacts/rich-html-new-read-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_paragraph_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Paragraphs.mh"
click "编辑 HTML 文本 First paragraph"
down 292 121
move 304 165
up 304 165
layout
shot "$artifacts/rich-html-cross-block-selected-1200.png"
type 新
layout
shot "$artifacts/rich-html-cross-block-edited-1200.png"
key cmd+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_structure_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Structure.mh"
click "编辑 HTML 文本 BodyText"
key home
key right
key right
key right
key right
key enter
layout
shot "$artifacts/rich-html-split-740.png"
key cmd+s
quit
EOF

if ! cmp -s "$rich_structure_fixture/Structure.mh" <(printf '<p>Body</p>\n<p>Text</p>\n<!--keep-->\n'); then
    printf 'Rich paragraph Enter did not preserve exact source bytes.\n' >&2
    exit 1
fi

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_structure_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Structure.mh"
click "编辑 HTML 文本 Text"
key home
key backspace
layout
shot "$artifacts/rich-html-merged-740.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_styled_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Styled.mh"
click "编辑 HTML 文本 bold"
key home
key right
key right
key enter
layout
shot "$artifacts/rich-html-styled-split-740.png"
type 新
layout
shot "$artifacts/rich-html-styled-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_boundary_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Boundary.mh"
click "编辑 HTML 文本 before "
key end
key enter
layout
shot "$artifacts/rich-html-boundary-split-740.png"
type 新
layout
shot "$artifacts/rich-html-boundary-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_merge_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Merge.mh"
click "编辑 HTML 文本 ld"
key home
key backspace
layout
shot "$artifacts/rich-html-styled-merged-740.png"
type 新
layout
shot "$artifacts/rich-html-styled-merge-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_list_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "List.mh"
click "编辑 HTML 文本 AlphaBeta"
key home
key right
key right
key right
key right
key right
key enter
layout
shot "$artifacts/rich-html-list-split-740.png"
type 新
layout
shot "$artifacts/rich-html-list-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_table_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Table.mh"
click "编辑 HTML 文本 B"
key tab
layout
shot "$artifacts/rich-html-table-new-row-740.png"
type 新
layout
shot "$artifacts/rich-html-table-row-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_exit_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Exit.mh"
click "编辑 HTML 文本 "
key enter
layout
shot "$artifacts/rich-html-list-exit-740.png"
type 新
layout
shot "$artifacts/rich-html-list-exit-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_middle_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Middle.mh"
click "编辑 HTML 文本 "
key enter
layout
shot "$artifacts/rich-html-list-middle-exit-740.png"
type 新
layout
shot "$artifacts/rich-html-list-middle-typed-740.png"
key cmd+z
key cmd+z
key cmd+shift+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$rich_exit_roundtrip_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Roundtrip.mh"
click "编辑 HTML 文本 "
key enter
key backspace
layout
shot "$artifacts/rich-html-list-exit-reversed-740.png"
key cmd+z
key cmd+shift+z
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$source_code_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "main.go"
layout
shot "$artifacts/code-go-light-1200.png"
click "明暗"
layout
shot "$artifacts/code-go-dark-1200.png"
resize 740 520
layout
shot "$artifacts/code-go-narrow-740.png"
click "文内查找"
type return
layout
shot "$artifacts/code-find-narrow-740.png"
click "替换文本"
type yield
click "替换当前匹配"
layout
shot "$artifacts/code-replaced-narrow-740.png"
click "代码编辑器"
key cmd+z
click "文内查找"
key esc
click "literal.txt"
layout
shot "$artifacts/plain-literal-740.png"
key cmd+3
layout
shot "$artifacts/plain-stays-source-740.png"
click "page.html"
layout
shot "$artifacts/html-source-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$media_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Media.md"
click "阅读"
layout
shot "$artifacts/markdown-image-light-1200.png"
click "查看图片 本地图"
layout
shot "$artifacts/markdown-inspector-light-1200.png"
click "放大图片"
layout
shot "$artifacts/markdown-inspector-zoom-1200.png"
key esc
click "明暗"
layout
shot "$artifacts/markdown-image-dark-1200.png"
click "查看图片 本地图"
layout
shot "$artifacts/markdown-inspector-dark-1200.png"
resize 740 520
layout
shot "$artifacts/markdown-inspector-dark-740.png"
key esc
layout
shot "$artifacts/markdown-image-dark-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$media_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Mixed.md"
click "阅读"
layout
shot "$artifacts/markdown-inline-light-1200.png"
click "查看图片 彩条"
layout
shot "$artifacts/markdown-inline-first-inspector-1200.png"
key esc
click "查看图片 青图"
layout
shot "$artifacts/markdown-inline-second-inspector-1200.png"
key esc
click "分栏"
layout
shot "$artifacts/markdown-inline-split-1200.png"
click "阅读"
click "明暗"
resize 740 520
layout
shot "$artifacts/markdown-inline-dark-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$settings_file" acceptance_run ./build/markstack-headless "$initial_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "文件"
click "设置"
layout
shot "$artifacts/settings-light-1200.png"
resize 740 520
click "增大源码字号"
click "增大源码字号"
click "增大阅读字号"
click "柔和阅读主题"
click "使用深色外观"
layout
shot "$artifacts/settings-dark-740.png"
key esc
click "Welcome.md"
layout
shot "$artifacts/source-20-dark-740.png"
click "阅读"
layout
shot "$artifacts/reading-soft-dark-740.png"
quit
EOF
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$work/soft-light/settings.json" acceptance_run ./build/markstack-headless "$initial_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "文件"
click "设置"
click "增大阅读字号"
click "柔和阅读主题"
key esc
click "Welcome.md"
click "阅读"
layout
shot "$artifacts/reading-soft-light-1200.png"
quit
EOF
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$work/soft-rich/settings.json" acceptance_run ./build/markstack-headless "$rich_search_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "文件"
click "设置"
click "增大阅读字号"
click "柔和阅读主题"
key esc
click "Rich-HTML.mh"
click "阅读"
layout
shot "$artifacts/rich-reading-soft-light-740.png"
quit
EOF
if ! jq -e '.version == 1 and .dark == true and .sourceSize == 20 and .readingSize == 18 and .readingTheme == "soft"' "$settings_file" >/dev/null; then
    printf 'Settings did not persist the selected values.\n' >&2
    exit 1
fi
cp "$settings_file" "$artifacts/settings-valid.json"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$settings_file" acceptance_run ./build/markstack-headless "$initial_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "文件"
click "设置"
layout
shot "$artifacts/settings-restored-740.png"
quit
EOF
if ! cmp -s "$settings_file" "$artifacts/settings-valid.json"; then
    printf 'Restart unexpectedly rewrote settings.\n' >&2
    exit 1
fi
printf '{bad' > "$settings_file"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$settings_file" acceptance_run ./build/markstack-headless "$initial_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "文件"
click "设置"
click "使用深色外观"
layout
shot "$artifacts/settings-corrupt-740.png"
quit
EOF
if ! cmp -s "$settings_file" <(printf '{bad'); then
    printf 'Corrupt settings were overwritten by a UI action.\n' >&2
    exit 1
fi

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$root/examples" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
key cmd+shift+f
shot "$artifacts/workspace-search-empty-740.png"
type Mixed
key enter
waitfor "7:9  plain **mixed**" 5000
layout
shot "$artifacts/workspace-search-results-740.png"
click "7:9  plain **mixed**"
settle 100
editor
shot "$artifacts/workspace-search-open-740.png"
resize 1200 780
layout
shot "$artifacts/workspace-search-open-1200.png"
resize 740 520
click "明暗"
layout
shot "$artifacts/workspace-search-dark-740.png"
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$selection_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Split.md"
click "排版"
click "编辑 BodyText"
key shift+left
key shift+left
key shift+left
key shift+left
click "加粗选区"
shot "$artifacts/rendered-selected-bold.png"
type X
shot "$artifacts/rendered-selected-bold-input.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$italic_selection_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Mixed.md"
click "排版"
click "编辑 link tail"
key shift+left
key shift+left
key shift+left
key shift+left
click "斜体选区"
shot "$artifacts/rendered-selected-italic.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$link_selection_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 1200 780
click "Rendered-Mixed.md"
click "排版"
click "编辑 before bold after"
key shift+left
key shift+left
key shift+left
key shift+left
key shift+left
click "链接选区"
type https://example.com/new
key enter
shot "$artifacts/rendered-selected-link.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$strike_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Split.md"
click "排版"
click "编辑 BodyText"
key shift+left
key shift+left
key shift+left
key shift+left
click "删除线选区"
shot "$artifacts/rendered-selected-strike.png"
type X
shot "$artifacts/rendered-strike-input.png"
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$block_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "标题级别"
shot "$artifacts/rendered-block-menu.png"
click "引用"
shot "$artifacts/rendered-block-quote.png"
click "标题级别"
click "无序列表"
shot "$artifacts/rendered-block-bullets.png"
click "标题级别"
click "有序列表"
shot "$artifacts/rendered-block-ordered.png"
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$task_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "标题级别"
click "任务列表"
shot "$artifacts/rendered-task-open.png"
click "任务 Body"
shot "$artifacts/rendered-task-checked.png"
type X
layout
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$code_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "标题级别"
click "代码块"
shot "$artifacts/rendered-code-command-source.png"
click "阅读"
shot "$artifacts/rendered-code-command-preview.png"
key cmd+s
quit
EOF

VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$table_fixture" <<EOF | tee -a "$artifacts/protocol.log"
resize 740 520
click "Rendered-Enter.md"
click "排版"
click "编辑 Body"
click "标题级别"
click "插入表格"
shot "$artifacts/rendered-table-command-source.png"
click "阅读"
shot "$artifacts/rendered-table-command-preview.png"
layout
key cmd+s
quit
EOF

if rg -q '^error' "$artifacts/protocol.log"; then
    printf 'Headless protocol reported errors.\n' >&2
    exit 1
fi
unexpected=""
while IFS= read -r problem; do
    if [[ "$problem" == *'"、" is drawn over "斜体"' ]]; then
        continue
    fi
    if [[ "$problem" == 'spill x=230 y=253 w=480 h=26 "证活动输入仍在排版画布内。长段落包含多个汉字和自然换行位置，" reaches out of "渲染块编辑器" at x=230 y=175 w=470 h=156' ]]; then
        continue
    fi
    if [[ "$problem" == 'spill x=300.8 y=271 w=805.61725 h=26 "打开一个本地文件夹，左侧会列出其中的 Markdown 文档。选择文档即可编辑，分栏模式可以同时阅读排版结果。" reaches out of "编辑 打开一个本地文件夹，左侧会列出其中的 Markdown 文档。选择文档即可编辑，分栏模式可以同时阅读排版结果。" at x=300.8 y=271 w=800.00006 h=26' ]]; then
        continue
    fi
    unexpected+="$problem"$'\n'
done < <(rg '^(offscreen|clipped|overlap|spill|collide) ' "$artifacts/protocol.log" || true)
if [[ -n "$unexpected" ]]; then
    printf 'Unexpected layout diagnostics:\n%s' "$unexpected" >&2
    exit 1
fi
expected=$'# Acceptance draft\n中文验收'
for file in "$work/draft.md" "$work/copy.md"; do
    if [[ "$(<"$file")" != "$expected" ]]; then
        printf 'Saved document differs: %s\n' "$file" >&2
        exit 1
    fi
done
if [[ "$(head -n 1 "$initial_fixture/Welcome.md")" != "# 墨栈笔记" ]] || ! cmp -s <(tail -n +2 examples/Welcome.md) <(tail -n +2 "$initial_fixture/Welcome.md"); then
    printf 'Rendered edit changed bytes outside the first heading.\n' >&2
    exit 1
fi
if ! rg -q 'value="墨栈笔记"' "$artifacts/protocol.log"; then
    printf 'Native rendered input did not receive the heading edit.\n' >&2
    exit 1
fi
if ! rg -Fq 'ok "\n一处整理，一处写作。"' "$artifacts/protocol.log"; then
    printf 'Cross-block copy did not place visible text on the clipboard.\n' >&2
    exit 1
fi
if ! rg -q 'value="Keep me"' "$artifacts/protocol.log"; then
    printf 'Canceling close did not retain the draft.\n' >&2
    exit 1
fi
if ! rg -Fq 'selection=71,76 secure=false' "$artifacts/protocol.log"; then
    printf 'Workspace search did not focus and select its Markdown match.\n' >&2
    exit 1
fi
if ! cmp -s "$rendered_fixture/Rendered-Enter.md" <(printf '# Title\n\nBody\n\nNext\n\nThird\n\nTail\n'); then
    printf 'Rendered paragraph insertion changed adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$split_fixture/Rendered-Split.md" <(printf '# Title\n\nBody\n\nNewText\n\nTail\n'); then
    printf 'Rendered interior split changed adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$merge_fixture/Rendered-Enter.md" <(printf '# Title\n\nBodyTail\n'); then
    printf 'Rendered paragraph merge changed adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$heading_fixture/Rendered-Enter.md" <(printf '# Title\n\nIntro\n\nBody\n\nTail\n'); then
    printf 'Rendered heading Enter changed adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$inline_fixture/Rendered-Inline.md" <(printf '# Title\n\n**boldX**\n\n[labelY](https://example.invalid/preserved)\n\nplain **mixed**\n'); then
    printf 'Rendered inline edit lost Markdown markers or link destination.\n' >&2
    exit 1
fi
if ! cmp -s "$bold_fixture/Rendered-Enter.md" <(printf '# Title\n\n**BodyX**\n\nTail\n'); then
    printf 'Rendered bold command did not retain editable source.\n' >&2
    exit 1
fi
if ! cmp -s "$italic_fixture/Rendered-Enter.md" <(printf '# Title\n\n*BodyX*\n\nTail\n'); then
    printf 'Rendered italic command did not retain editable source.\n' >&2
    exit 1
fi
if ! cmp -s "$link_fixture/Rendered-Enter.md" <(printf '# Title\n\n[BodyX](https://example.com/doc)\n\nTail\n'); then
    printf 'Rendered link command did not preserve URL and editable label.\n' >&2
    exit 1
fi
if ! cmp -s "$list_fixture/Rendered-List.md" <(printf '%s\n' '- one' '- two' '- new' '- three'); then
    printf 'Rendered list continuation changed neighboring items.\n' >&2
    exit 1
fi
if ! cmp -s "$list_exit_fixture/Rendered-List.md" <(printf '%s\n' '- one' '- two' '' 'Body' '- three'); then
    printf 'Rendered list exit changed neighboring items.\n' >&2
    exit 1
fi
if ! cmp -s "$blank_fixture/Rendered-Enter.md" <(printf '# Title\n\nBody\n\n\nNext\n\nTail\n'); then
    printf 'Second empty paragraph Enter rewrote the source.\n' >&2
    exit 1
fi
if ! cmp -s "$ordered_fixture/Rendered-Ordered.md" <(printf '%s\n' '8. one' '9. two' '10. three'); then
    printf 'Rendered ordered list did not increment number.\n' >&2
    exit 1
fi
if ! cmp -s "$mixed_fixture/Rendered-Mixed.md" <(printf '# Title\n\nbefore **bold** afterX\n\n[link](https://example.invalid/preserved) tail\n\nTail\n'); then
    printf 'Rendered mixed inline edit changed untouched markup.\n' >&2
    exit 1
fi
if ! cmp -s "$selection_fixture/Rendered-Split.md" <(printf '# Title\n\nBody**X**\n\nTail\n'); then
    printf 'Selected bold lost the source marker or continued input.\n' >&2
    exit 1
fi
if ! cmp -s "$italic_selection_fixture/Rendered-Mixed.md" <(printf '# Title\n\nbefore **bold** after\n\n[link](https://example.invalid/preserved) *tail*\n\nTail\n'); then
    printf 'Selected italic changed the link destination.\n' >&2
    exit 1
fi
if ! cmp -s "$link_selection_fixture/Rendered-Mixed.md" <(printf '# Title\n\nbefore **bold** [after](https://example.com/new)\n\n[link](https://example.invalid/preserved) tail\n\nTail\n'); then
    printf 'Selected link changed adjacent formatting or existing URL.\n' >&2
    exit 1
fi
if ! cmp -s "$strike_fixture/Rendered-Split.md" <(printf '# Title\n\nBody~~X~~\n\nTail\n'); then
    printf 'Selected strikethrough changed adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$block_fixture/Rendered-Enter.md" <(printf '# Title\n\n1. Body\n\nTail\n'); then
    printf 'Block formatting changed adjacent Markdown source.\n' >&2
    exit 1
fi
if ! cmp -s "$task_fixture/Rendered-Enter.md" <(printf '# Title\n\n- [x] BodyX\n\nTail\n'); then
    printf 'Rendered task list changed its checkbox or label source.\n' >&2
    exit 1
fi
if ! cmp -s "$code_fixture/Rendered-Enter.md" <(printf '# Title\n\n```\nBody\n```\n\nTail\n'); then
    printf 'Code block command rewrote adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$fenced_fixture/Fenced-Code.md" <(printf '# Native code\n\n```go\npackage main\nfunc main() { println("hello") }新\n尾\n```\n\nTail\n'); then
    printf 'Rendered fenced-code edit changed its fence or neighboring Markdown.\n' >&2
    exit 1
fi
if ! cmp -s "$table_fixture/Rendered-Enter.md" <(printf '# Title\n\nBody\n\n| 列 1 | 列 2 |\n| --- | --- |\n|     |     |\n\nTail\n'); then
    printf 'Table command rewrote adjacent source.\n' >&2
    exit 1
fi
if ! cmp -s "$mixed_wrapped_fixture/Rendered-Mixed-Long.md" examples/Rendered-Mixed-Long.md; then
    printf 'Wrapped mixed editing changed disk bytes after undo.\n' >&2
    exit 1
fi
if [[ -e "$tree_fixture/draft.md" ]] || ! cmp -s "$tree_fixture/renamed.md" <(printf '# Native draft') || ! cmp -s "$tree_fixture/Sample.md" examples/Rendered-Enter.md; then
    printf 'File-tree create, rename, or canceled Trash changed unexpected bytes.\n' >&2
    exit 1
fi
if ! cmp -s "$drag_fixture/drag-valid.md" examples/Welcome.md || ! cmp -s "$drag_workspace/in-folder.md" examples/Rendered-Enter.md; then
    printf 'Drag-open or canceled workspace switch changed source bytes.\n' >&2
    exit 1
fi
if [[ ! -f "$recent_store" ]] || ! rg -q '"version":1' "$recent_store" || ! rg -q 'drag-valid.md' "$recent_store" || rg -q '一处整理' "$recent_store"; then
    printf 'Recent history was missing, stale, or contained document content.\n' >&2
    exit 1
fi
if ! cmp -s "$template_fixture/existing.md" examples/Rendered-Split.md || ! cmp -s "$template_fixture/note.md" <(printf '# 项目笔记\n\n## 摘要\n\n## 正文\n\n## 参考\n\n') || ! cmp -s "$template_fixture/proposal.md" <(printf '# 方案\n\n## 背景\n\n## 目标\n\n## 实施步骤\n\n## 验收标准\n\n'); then
    printf 'Template Save As changed existing data or wrote unexpected Markdown.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_search_fixture/Rich-HTML.mh" internal/richedit/testdata/Rich-HTML.mh; then
    printf 'Rich HTML search or mode switching changed original source bytes.\n' >&2
    exit 1
fi
if ! cmp -s "$real_rich_fixture/Real-MkDown.mh" internal/richedit/testdata/Real-MkDown.mh; then
    printf 'Real MkDown no-op save changed original source bytes.\n' >&2
    exit 1
fi
if ! cmp -s "$real_structure_fixture/Real-MkDown-Structure.mh" internal/richedit/testdata/Real-MkDown-Structure.mh; then
    printf 'Real MkDown table undo changed original source bytes.\n' >&2
    exit 1
fi
if ! cmp -s <(head -n 1 "$rich_cross_fixture/Rich-HTML.mh") <(printf '%s\n' '<p data-id="note">A 新<strong>ld</strong> <em>斜体</em> <a href="https://example.org">link</a></p>') || ! cmp -s <(tail -n +2 "$rich_cross_fixture/Rich-HTML.mh") <(tail -n +2 internal/richedit/testdata/Rich-HTML.mh); then
    printf 'Cross-style rich edit changed untouched HTML or failed to save its source patch.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_new_fixture/draft.mh" <(printf '%s' '<p>开始</p>'); then
    printf 'New rich document wrote unexpected HTML bytes.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_paragraph_fixture/Paragraphs.mh" <(printf '<p>First p新 paragraph</p>\n<!--keep-->\n<p>Untouched tail</p>\n'); then
    printf 'Cross-paragraph rich edit changed untouched HTML or failed to save.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_structure_fixture/Structure.mh" internal/richedit/testdata/Structure.mh; then
    printf 'Rich paragraph Backspace did not restore original source bytes.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_styled_fixture/Styled.mh" <(printf '<p>before <strong>bo</strong></p>\n<p><strong>新ld</strong> after</p>\n<!--keep-->\n'); then
    printf 'Styled rich Enter changed an untouched source interval or lost redo history.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_boundary_fixture/Boundary.mh" <(printf '<p>before </p>\n<p><strong>新bold</strong> after</p>\n<!--keep-->\n'); then
    printf 'Boundary rich Enter produced empty style markup or changed untouched source.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_merge_fixture/Merge.mh" <(printf '<p>before <strong>bo新ld</strong> after</p>\n<!--keep-->\n'); then
    printf 'Styled Backspace merge changed untouched source or lost history.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_list_fixture/List.mh" <(printf '<ol start="9"><li>Alpha</li>\n<li>新Beta</li><li>Tail</li></ol>\n<!--keep-->\n'); then
    printf 'Rich list Enter changed numbering, adjacent source, or undo history.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_table_fixture/Table.mh" <(printf '<table><tbody><tr><td>A</td><td>B</td></tr>\n<tr><td>新</td><td><br></td></tr></tbody></table>\n<!--keep-->\n'); then
    printf 'Rich table Tab changed old cells, unknown tail, or undo history.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_exit_fixture/Exit.mh" <(printf '<ol start="9"><li>First</li></ol>\n<p>新</p>\n<!--keep-->\n'); then
    printf 'Empty rich list exit changed numbering, unknown tail, or undo history.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_middle_fixture/Middle.mh" <(printf '<ol start="9"><li>one</li></ol>\n<p>新</p>\n<ol start="11"><li>three</li></ol>\n<!--keep-->\n'); then
    printf 'Middle rich list exit changed later numbering or unknown source.\n' >&2
    exit 1
fi
if ! cmp -s "$rich_exit_roundtrip_fixture/Roundtrip.mh" <(printf '<ol start="9"><li>one</li><li><br></li><li>three</li></ol>\n<!--keep-->\n'); then
    printf 'Enter then Backspace did not restore original rich list bytes.\n' >&2
    exit 1
fi
for name in main.go literal.txt page.html; do
    if ! cmp -s "$source_code_fixture/$name" "cmd/markstack/testdata/code/$name"; then
        printf 'Code/plain viewing changed original %s bytes.\n' "$name" >&2
        exit 1
    fi
done
if ! cmp -s "$media_fixture/Media.md" "$work/media-original.md" || ! cmp -s "$media_fixture/Mixed.md" "$work/mixed-original.md" || ! cmp -s "$media_fixture/assets/p.png" "$work/media-original.png" || ! cmp -s "$media_fixture/assets/q.png" "$work/media-q-original.png"; then
    printf 'Markdown image preview changed source or local PNG bytes.\n' >&2
    exit 1
fi
auto_fixture="$work/auto-save"
auto_settings="$work/auto-settings/settings.json"
auto_recovery="$work/auto-recovery/recovery.json"
mkdir -p "$auto_fixture"
printf '\357\273\277original\r\n' > "$auto_fixture/Auto.md"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$auto_settings" MARKSTACK_RECOVERY_PATH="$auto_recovery" acceptance_run ./build/markstack-headless "$auto_fixture" <<EOF | tee "$artifacts/auto-save-protocol.log"
click "Auto.md"
click "文件"
click "设置"
click "自动保存"
key esc
click "Markdown 源码编辑器"
key cmd+a
type updated
settle 1300
editor
quit
EOF
if ! cmp -s "$auto_fixture/Auto.md" <(printf '\357\273\277updated') || ! jq -e '.autoSave == true' "$auto_settings" >/dev/null || [[ -e "$auto_recovery" ]]; then
    printf 'Enabled auto-save failed to write BOM-preserving source or clear recovery.\n' >&2
    exit 1
fi
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$auto_settings" MARKSTACK_RECOVERY_PATH="$auto_recovery" acceptance_run ./build/markstack-headless "$auto_fixture" <<EOF | tee -a "$artifacts/auto-save-protocol.log"
click "Auto.md"
click "文件"
click "设置"
click "自动保存"
key esc
click "Markdown 源码编辑器"
key cmd+a
type disabled draft
settle 1300
editor
quit
EOF
if ! cmp -s "$auto_fixture/Auto.md" <(printf '\357\273\277updated') || ! jq -e '.autoSave == false' "$auto_settings" >/dev/null || ! jq -e '.drafts[0].body == "disabled draft"' "$auto_recovery" >/dev/null; then
    printf 'Disabled auto-save changed disk or lost the dirty recovery draft.\n' >&2
    exit 1
fi
jq '.autoSave = true' "$auto_settings" > "$work/auto-settings-enabled.json"
install -m 0600 "$work/auto-settings-enabled.json" "$auto_settings"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$auto_settings" MARKSTACK_RECOVERY_PATH="$auto_recovery" acceptance_run ./build/markstack-headless "$auto_fixture" <<EOF | tee -a "$artifacts/auto-save-protocol.log"
settle 1300
editor
quit
EOF
if ! cmp -s "$auto_fixture/Auto.md" <(printf '\357\273\277updated') || ! jq -e '.drafts[0].body == "disabled draft"' "$auto_recovery" >/dev/null; then
    printf 'Restart auto-saved a recovered draft without a new edit.\n' >&2
    exit 1
fi
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$auto_settings" MARKSTACK_RECOVERY_PATH="$auto_recovery" acceptance_run ./build/markstack-headless "$auto_fixture" <<EOF | tee -a "$artifacts/auto-save-protocol.log"
click "Markdown 源码编辑器"
key cmd+a
type recovered edited
settle 1300
editor
quit
EOF
if ! cmp -s "$auto_fixture/Auto.md" <(printf '\357\273\277recovered edited') || [[ -e "$auto_recovery" ]]; then
    printf 'New edit after recovery did not auto-save or clear the journal.\n' >&2
    exit 1
fi
conflict_fixture="$work/auto-conflict"
conflict_settings="$work/conflict-settings/settings.json"
conflict_recovery="$work/conflict-recovery/recovery.json"
mkdir -p "$conflict_fixture"
printf 'original\n' > "$conflict_fixture/Conflict.md"
mkfifo "$work/conflict-in" "$work/conflict-out"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 MARKSTACK_SETTINGS_PATH="$conflict_settings" MARKSTACK_RECOVERY_PATH="$conflict_recovery" acceptance_run ./build/markstack-headless "$conflict_fixture" < "$work/conflict-in" > "$work/conflict-out" &
conflict_pid=$!
exec 8> "$work/conflict-in"
exec 9< "$work/conflict-out"
IFS= read -r reply <&9
printf '%s\n' "$reply" >> "$artifacts/auto-save-protocol.log"
conflict_command() {
    printf '%s\n' "$1" >&8
    IFS= read -r reply <&9
    printf '%s\n' "$reply" >> "$artifacts/auto-save-protocol.log"
    [[ "$reply" == ok* ]]
}
conflict_command 'click "Conflict.md"'
conflict_command 'click "文件"'
conflict_command 'click "设置"'
conflict_command 'click "自动保存"'
conflict_command 'key esc'
conflict_command 'click "Markdown 源码编辑器"'
conflict_command 'key cmd+a'
conflict_command 'type local draft'
printf 'external\n' > "$conflict_fixture/Conflict.md"
conflict_command 'settle 1300'
conflict_command 'editor'
conflict_command 'quit'
exec 8>&-
exec 9<&-
wait "$conflict_pid"
if ! cmp -s "$conflict_fixture/Conflict.md" <(printf 'external\n') || ! jq -e '.drafts[0].body == "local draft"' "$conflict_recovery" >/dev/null; then
    printf 'Auto-save conflict overwrote an external edit or discarded the draft.\n' >&2
    exit 1
fi
table_cell_fixture="$work/rendered-table-cell"
mkdir -p "$table_cell_fixture"
printf '\357\273\277# Head\r\n\r\n| A | B |\r\n| --- | --- |\r\n| old |  |\r\n\r\nTail\r\n' > "$table_cell_fixture/Table.md"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$table_cell_fixture" <<EOF | tee "$artifacts/table-cell-protocol.log"
resize 740 520
click "Table.md"
click "排版"
click "id:markstack/rendered-table-cells/#0"
type 新
key tab
key tab
key tab
compose pin
key cmd+s
expect text "finish text composition before saving" within 1000
type 乙
expect no-text "finish text composition before saving" within 1000
key shift+tab
key tab
key cmd+z
editor
key cmd+shift+z
editor
shot "$artifacts/rendered-table-cell-active-740.png"
layout
key cmd+s
quit
EOF
if ! rg -q 'ok value="" selection=' "$artifacts/table-cell-protocol.log" || ! rg -q 'ok value="乙" selection=' "$artifacts/table-cell-protocol.log" ||
    ! cmp -s "$table_cell_fixture/Table.md" <(printf '\357\273\277# Head\r\n\r\n| A新 | B |\r\n| --- | --- |\r\n| old |  乙|\r\n\r\nTail\r\n'); then
    printf 'Rendered table cell editing changed BOM, CRLF, padding, alignment or untouched source.\n' >&2
    exit 1
fi
aligned_fixture="$root/build/acceptance/aligned-cell-fixture"
mkdir -p "$aligned_fixture"
printf '\357\273\277# Head\r\n\r\n| Center | Right |\r\n| :---: | ---: |\r\n| middle | data |\r\n\r\nTail\r\n' > "$aligned_fixture/Table.md"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$aligned_fixture" <<EOF | tee "$artifacts/aligned-cell-protocol.log"
resize 740 520
click "Table.md"
click "排版"
shot "$artifacts/rendered-aligned-cell-idle-740.png"
click "id:markstack/rendered-table-cells/#3"
compose nihaoshijie
shot "$artifacts/rendered-aligned-cell-preedit-740.png"
layout
key cmd+s
expect text "finish text composition before saving" within 1000
type 你好世界
expect no-text "finish text composition before saving" within 1000
shot "$artifacts/rendered-aligned-cell-committed-740.png"
key cmd+z
editor
key cmd+shift+z
editor
type 尾
editor
key cmd+s
quit
EOF
if ! rg -q 'ok value="data" selection=' "$artifacts/aligned-cell-protocol.log" || ! rg -q 'ok value="data你好世界" selection=' "$artifacts/aligned-cell-protocol.log" || ! rg -q 'ok value="data你好世界尾" selection=' "$artifacts/aligned-cell-protocol.log" ||
    ! cmp -s "$aligned_fixture/Table.md" <(printf '\357\273\277# Head\r\n\r\n| Center | Right |\r\n| :---: | ---: |\r\n| middle | data你好世界尾 |\r\n\r\nTail\r\n'); then
    printf 'Aligned table input changed BOM, CRLF, untouched cells, or Undo/Redo history.\n' >&2
    exit 1
fi
for state in idle preedit committed; do
    if ! node "$root/scripts/check-ui-screenshot.cjs" "$artifacts/rendered-aligned-cell-$state-740.png" 740 520; then
        printf 'Aligned table %s current screenshot is invalid.\n' "$state" >&2
        exit 1
    fi
done
styled_fixture="$root/build/acceptance/styled-composition-fixture"
mkdir -p "$styled_fixture"
printf '\357\273\277前 **boldword** tail\r\n\r\nTail\r\n<!--preserve-->\r\n' > "$styled_fixture/Styled.md"
VIVA_HEADLESS=1 VIVA_HEADLESS_SCALE=1 acceptance_run ./build/markstack-headless "$styled_fixture" <<EOF | tee "$artifacts/styled-composition-protocol.log"
resize 740 520
click "Styled.md"
click "排版"
shot "$artifacts/rendered-styled-reader-740.png"
click "id:markstack/rendered-rows/#0"
key home
key right
key right
key right
key right
key shift+right
key shift+right
compose 候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选候选
editor
shot "$artifacts/rendered-styled-preedit-740.png"
layout
key cmd+s
expect text "finish text composition before saving" within 1000
type 新
expect no-text "finish text composition before saving" within 1000
editor
shot "$artifacts/rendered-styled-committed-740.png"
key cmd+s
key cmd+z
editor
key cmd+s
quit
EOF
if ! rg -Fq 'ok value="前 bo新word tail" selection=' "$artifacts/styled-composition-protocol.log" ||
    ! rg -Fq 'ok value="前 boldword tail" selection=' "$artifacts/styled-composition-protocol.log" ||
    ! cmp -s "$styled_fixture/Styled.md" <(printf '\357\273\277前 **boldword** tail\r\n\r\nTail\r\n<!--preserve-->\r\n'); then
    printf 'Styled preedit commit/Undo changed formatting, BOM, CRLF or untouched source.\n' >&2
    exit 1
fi
MARKSTACK_AFFINITY_ARTIFACT_DIR="$artifacts" acceptance_run go test -tags vivahead ./cmd/markstack -run '^TestRenderedLineEdgeArtifacts$' -count=1 -v | tee "$artifacts/line-edge-tests.log"
for state in end selection; do
    if ! node "$root/scripts/check-ui-screenshot.cjs" "$artifacts/rendered-line-edge-$state-740.png" 740 520; then
        printf 'Rendered line-edge %s current screenshot is invalid.\n' "$state" >&2
        exit 1
    fi
done
MARKSTACK_COMPOSITION_ARTIFACT_DIR="$artifacts" acceptance_run go test -tags vivahead ./cmd/markstack -run '^TestRenderedCrossStylePreeditArtifacts$' -count=1 -v | tee "$artifacts/cross-style-tests.log"
for state in preedit committed; do
    if ! node "$root/scripts/check-ui-screenshot.cjs" "$artifacts/rendered-cross-style-$state-740.png" 740 520; then
        printf 'Rendered cross-style %s current screenshot is invalid.\n' "$state" >&2
        exit 1
    fi
done
rg --files cmd internal scripts assets | LC_ALL=C sort | xargs shasum -a 256 > "$artifacts/source-manifest.txt"
shasum -a 256 go.mod go.sum viva.toml >> "$artifacts/source-manifest.txt"
printf 'PASS: functional acceptance; review screenshots and layout diagnostics in %s\n' "$artifacts"
printf 'HTML export artifacts: %s\n' "$artifacts/html-export"
