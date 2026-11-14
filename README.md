# 墨栈 / MarkStack

本地优先的 Go 1.26 + [Viva](https://github.com/vivaui/viva) 原生文档工作区，提供文件树、标签、Markdown 排版/源码/分栏、`.mh` 富文本和常见代码/纯文本编辑。没有 WebView、账号或云服务。Viva 的固定版本以 `go.mod` 为准；本机开发包不是正式发布版。

## 运行

```sh
go run ./cmd/markstack
go run ./cmd/markstack ./examples
```

首次打开不会写入用户文档。文件菜单提供工作区/文件打开、模板、保存、HTML/PDF 导出、最近打开和设置；支持 Markdown、`.mh`、常见 Go/JS/TS/JSON/YAML/HTML/SVG/CSS/Python/SQL/shell 源码及 txt/log/csv/tsv 字面文本，Office 文件不在范围。新文档保存弹系统选择器，已有文档按 ⌘S 就地保存；另存为不能悄悄转换文档种类或覆盖已有目标。文件树可新建、重命名并确认移入系统回收站；⌘1 / ⌘2 / ⌘3 切换源码、分栏、阅读，⌘F 筛选文件名，⌘⇧F 搜索工作区，⌘⌥F 查当前文档。代码/纯文本提供文件内替换、缩进和撤销；导出不改变当前标签或磁盘原文。PDF 默认使用内置 Noto Sans SC Regular 字体，`MARKSTACK_PDF_FONT` 可选填其他 Unicode `.ttf`；字体的 OFL 1.1 许可位于 `internal/exportpdf/font/OFL.txt`，本机包内也有 `Contents/Resources/PDF-Font-OFL.txt`。

## 验证

```sh
go test ./...
go build ./cmd/markstack
bash scripts/acceptance.sh
```

macOS 离屏截图需要 `vivahead` 构建标签：

```sh
go build -tags vivahead -o build/markstack-headless ./cmd/markstack
VIVA_HEADLESS=1 ./build/markstack-headless ./examples
```

行协议可发送 `texts`、`layout`、`shot /tmp/markstack.png` 和 `quit`。截图、Spec、计划、视觉记录和验收账本仅保存在本地，不包含在公开仓库、PR 或 Issue 附件中。按最新 UI 核对内容、布局和交互，不与旧金图比较；自动化、真实原生输入、其它平台和正式发行分别验收。

完整受控脚本运行竞态测试、静态检查、输入和保存流程，把本地产物写到忽略的 `build/acceptance/latest/`。运行前使用独立测试工作区，保留命令、版本、失败/跳过项和字节哈希；不以新截图自我对比代替验收。

普通启动会把未保存草稿写到用户配置目录下私有的 `MarkStack/recovery.json`，下次启动只恢复脏标签，不自动写回原 Markdown。外部文件已变化、被删除或替换为符号链接时，就地保存会提示冲突，仍可另存为新路径。恢复快照在后台合并编辑突发，约 250ms 安静后写入；突发崩溃可能丢失尚未落盘的最近输入。`VIVA_HEADLESS=1` 默认不接触用户 journal；测试恢复时须显式设置隔离的 `MARKSTACK_RECOVERY_PATH`。
普通启动另以私有 `MarkStack/session.json` 记录上次工作区、已命名标签顺序、活动标签和光标；干净标签从当前磁盘重新加载，脏稿仍以 `recovery.json` 为准。显式指定工作区时不自动恢复上次会话；headless 测试须设置隔离的 `MARKSTACK_SESSION_PATH` 才会启用会话快照。

用 `bash scripts/package.sh --no-sign` 生成本机 `build/MarkStack.app` 并附上字体许可证；未签名应用仅供本机验证，正式签名发布流程仍未建立。

主包采用 Cobalt Mark 蓝色折带图标；`viva.toml` 与运行窗口共享 `assets/markstack-icon.png`，平台打包时生成 `AppIcon.icns`。功能图标采用编译后的 Lucide 矢量资源，生成命令和许可证见 [assets/README.md](assets/README.md)。`swift scripts/generate_icon.swift assets/markstack-icon.png build/icon-64.png 64` 可导出尺寸校验图；历史 Dock 截图仍保留其原版本含义。

## 当前边界

Markdown 排版编辑支持安全的标题、段落、引用、列表、行内样式、跨简单块选区及源区间撤销；普通围栏代码块和简单 GFM 表格单元格也可原生编辑，复杂嵌套与无法安全映射的结构显式回到源码。独立或普通文本混排的本地 PNG/JPEG/GIF 可受限预览、点击查看；缺失、远程及复杂格式图片显示字面或不可用状态。`.mh` 支持安全叶子编辑、普通段落和同列表项的结构拆分/合并、空项退出到普通段落、简单跨段与样式内部输入；简单表格可用 Tab/Shift+Tab 导航，末格 Tab 安全新增空行，复杂结构仍须源码模式。未知/危险节点只读并保留源字节。只无损编辑 UTF-8（可含 BOM/CRLF），单文档上限 16 MiB；非 UTF-8 拒绝改写。HTML/PDF 导出、外观/字号/阅读主题已加入；PDF 的 Linux/Windows 实机流程、Mermaid/公式等仍未完成，Office 不兼容。部分普通段落和表格格子已有真实 macOS 拼音候选证据，但跨复杂块输入、Typora/码克固定同尺寸像素差分与完整产品验收仍缺，详见验收矩阵。

## 参与与自动化

开发采用 Issue → `/claim` 机器人确认 → 本地 Spec/计划 → 实现与回归 → 源码 PR/CI → 分层验收的流程，详见 [CONTRIBUTING.md](CONTRIBUTING.md)。公开 Issue 说明可公开的范围和验收条件；PR 不要求私有文档存在于提交中，禁止加入 docs、计划和截图。不能把仓库创建或 CI 绿灯视为产品全部完成。

GitHub Actions 定义了 Go 质量检查、macOS 测试/竞态检测、Linux/Windows headless 编译、PR 需求关联校验和 unsigned macOS 开发包。机器人默认使用内置身份，无需额外密钥；打包产物不是正式签名发行，完整本机视觉与输入法验收仍独立进行。
