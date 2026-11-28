<div align="center">

<img src="assets/markstack-icon.png" width="96" height="96" alt="墨栈 MarkStack Logo" />

# 墨栈 / MarkStack

**基于 Go 与 Viva 打造的极速、本地优先、跨平台原生 Markdown 文档工作区。**

[English](README.md) | 简体中文

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Platform](https://img.shields.io/badge/Platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey)](#构建与打包)
[![Zero WebView](https://img.shields.io/badge/Runtime-原生%20GPU%20渲染%20%28无%20WebView%29-brightgreen)](#核心亮点)

</div>

---

**墨栈（MarkStack）** 是一款使用 **Go 1.26** 与 **[Viva](https://github.com/vivaui/viva)** 原生构建的本地优先 Markdown 写作工作区。不依赖 Electron 或 WebView、无需账号与云端锁定，在 **macOS、Windows 和 Linux** 上提供原生 GPU 排版、离线公式与流程图渲染、专业 PDF/DOCX/HTML 导出以及极速本地知识库管理体验。

## 界面预览

<p align="center">
  <img src="assets/media/preview-editor.png" width="92%" alt="墨栈 MarkStack 主界面" />
</p>

<details open>
<summary><b>现代设置页（6 套主题色样）& 全局命令面板</b></summary>
<p align="center">
  <img src="assets/media/preview-settings.png" width="48%" alt="墨栈设置页" />
  <img src="assets/media/preview-palette.png" width="48%" alt="墨栈命令面板" />
</p>
</details>

## 核心亮点

| 模块 | 能力介绍 |
| :--- | :--- |
| **四态无缝写作体验** | 支持 **排版（所见即所得）/ 源码 / 分栏 / 沉浸阅读** 一键切换；内置 `/` 斜杠命令菜单、输入法安全自动触发语法、选区浮动格式条、打字机/专注模式与 GFM 表格快捷编辑。 |
| **纯 Go 离线公式与图表** | 原生离线渲染 **LaTeX 数学公式**（`$行内$` / `$$块级$$`）与 **Mermaid 流程图/时序图**（`flowchart` / `graph` / `sequenceDiagram`），零 WebView、100% 离线可用。 |
| **47+ 主流代码语法高亮** | 基于 `chroma/v2` 为 Markdown 围栏代码块（带顶部语言徽章与一键复制）及独立源码文件提供高亮，全面覆盖 `SQL`（`mysql`、`postgresql`、`plsql`、`sqlite`）、`Go`、`Java`、`Rust`、`C/C++`、`C#`、`Python`、`TypeScript`、`YAML`、`TOML`、`Dockerfile`、`Nginx`、`GraphQL`、`Protobuf`、`Diff`、`PowerShell` 等。 |
| **专业 PDF / DOCX / HTML 导出** | 纯 Go 导出 **PDF**（内置 Noto Sans SC 中文字体、页眉页脚页码、3 种版式：`标准` 适合日常文档、`紧凑` 适合速记清单、`正式` 采用更宽页边距与更舒展行距，适合正式报告/打印，支持正文可点击 `[TOC]` 目录页与阅读器 `/Outlines` 侧边书签）；纯 Go 导出 **DOCX**（保留 Word 原生六级标题大纲与内嵌本地图片）；离线自包含 **HTML** 导出。 |
| **6 套阅读主题与现代设置页** | 亮色/暗色双外观搭配 6 套阅读主题（`标准`、`柔和`、`GitHub`、`羊皮纸` 暖色纸面、`典雅` 高对比度纸面风格，适合长文沉浸阅读、`Nord` 护眼冷色调），带直观色样卡片预览、中英双语界面与独立界面字号调节。 |
| **本地图片管理与图片上传** | 默认粘贴图片直接保存在本地 `assets/` 目录（支持清理未引用的孤立图片、保护多文档共享图片）。如果你需要把文章发到博客、公众号或分享给他人，不想发本地路径，可在 **设置 → 图片上传** 中配置自己的自定义上传接口或对象存储（支持阿里云 OSS、Cloudflare R2、AWS S3、MinIO）：以后粘贴图片会自动上传并插入网络链接，断网或上传失败时可一键存回本地 `assets/` 不丢图，密钥单独以 `0600` 权限保存在本机私有目录。 |
| **工作区智能与数据安全** | 实时字数/阅读时长统计、可折叠筛选大纲、工作区 **反向链接（Backlinks）**、全局 **命令面板（`Primary+Shift+P`）**、**快速切文件（`Primary+P`）**、外部磁盘变更冲突保护、崩溃恢复快照与 Ed25519 多基线签名增量更新。 |

## 常用快捷键

> `Primary` 在 macOS 为 `⌘`（Command），在 Windows / Linux 为 `Ctrl`。

| 功能 | 快捷键 |
| :--- | :--- |
| 全局命令面板 | `Primary + Shift + P` |
| 快速打开文件 / 切换标签 | `Primary + P` |
| 打开设置 | `Primary + ,` |
| 保存 / 另存为 | `Primary + S` / `Primary + Shift + S` |
| 快速切换第 1–9 个标签页 | `Primary + 1..9` |
| 源码 / 分栏 / 阅读模式 | `Primary + Alt + 1` / `2` / `3` |
| 筛选文件名 / 工作区全文搜 / 文内查找 | `Primary + F` / `Primary + Shift + F` / `Primary + Alt + F` |

## 安装与首次打开（未签名版本）

当前预编译安装包暂未购买 Apple 公证或 Windows 代码签名证书，首次打开如遇系统拦截可按以下方式放行：

- **macOS**：将 `MarkStack.app` 拖入「应用程序（`/Applications`）」后，如提示“已损坏，无法打开”或“无法验证开发者”，在终端执行以下快捷命令移除隔离属性即可正常打开：
  ```sh
  xattr -dr com.apple.quarantine /Applications/MarkStack.app
  open /Applications/MarkStack.app
  ```
- **Windows**：若 SmartScreen 弹出保护提示，点击 **更多信息** → **仍要运行**（也可直接解压使用便携版 `.zip`）。
- **Linux**：使用以下命令安装 `.deb` 包：
  ```sh
  sudo apt install ./markstack_*.deb
  ```

## 构建与打包

需要 **Go 1.26+** 及 Viva 所需的平台基础环境。

```sh
# 从源码运行
go run ./cmd/markstack
go run ./cmd/markstack ./examples

# 运行测试
go test ./...

# 打包 macOS 本地应用（未签名）
bash scripts/package.sh --no-sign

# 打包 Linux .deb / Windows .exe 与便携 .zip
bash scripts/package-linux.sh --no-sign amd64
bash scripts/package-windows.sh --no-sign amd64
```

## 开源协议

墨栈（MarkStack）采用 **[Apache License 2.0](LICENSE)** 开源协议。

内置资源与第三方开源依赖均遵循各自许可协议：
- **Noto Sans SC Regular**（PDF 导出内置中文字体）：[SIL Open Font License 1.1](internal/exportpdf/font/OFL.txt)
- **Lucide Icons**：[ISC License](assets/lucide/LICENSE)
- **Viva**、**Chroma**、**Goldmark**、**go-pdf/fpdf** 及 `go.mod` 中声明的其他 Go 开源依赖
