# MarkStack Development Instructions (Cross-Platform Pragmatic Mode)

## 1. 核心定位与唯一事实源

- **唯一北极星目标**：打造在 **Windows、Linux、macOS 三大桌面平台**均流畅、顺手、可 **100% 日常替代 Typora** 的本地优先 Go + Viva 原生 Markdown 写作应用（无 WebView、无云账号、原文无损保存）。
- **权威计划入口**：始终以本地 `docs/specs/001-markstack/plan.md` 为唯一当前执行计划；`docs/specs/001-markstack/v0.2-design.md` 提供底层原文无损契约参考。旧版拆散的 `plan-v0.2-*.md` 与 `archive/` 仅作历史查阅，**不再执行其中的 T0–T8 逐像素差分与繁琐台账门禁**。
- **本地材料红线**：严禁将 `docs/`、`specs/`、`plans/`、`screenshots/`、`plan*.md`、验收日志或测试截图加入 Git 或上传至 Issue/PR。提交前运行 `node .github/scripts/repository-policy.cjs`。

## 2. 跨平台工程准则（Windows + Linux + macOS 一等公民）

- **一次编写，三端原生一致**：
  - 所有新增排版渲染、悬浮工具条、大纲、公式与流程图均基于纯 Go + Viva 跨平台 `gfx` / `ui` / `app` 接口实现，确保在 Windows、Linux、macOS 上行为一致；
  - 文件路径统一使用 `filepath`，文档读写严格保留原文件的换行符（Windows `CRLF` / Unix `LF`）与 `UTF-8 BOM`；快捷键自适应平台修饰键（macOS `Cmd` ↔ Windows/Linux `Ctrl`）；涉及回收站、重命名、导出字体、系统外观等平台差异处，必须保证三端实现完整（如已有 `rename_darwin.go` / `rename_linux.go` / Windows 路径兼容），不得破坏任一平台编译与运行。
- **高效的三端验证分工（拒绝本机堆积 rootfs 镜像）**：
  - **本地开发时**：跑当前平台聚焦单测 + 秒级三端静态/编译检查（`GOOS=windows GOARCH=amd64 go build ./...`、`GOOS=linux GOARCH=amd64 go build ./...`）；
  - **里程碑收口时**：通过 GitHub Actions 原生三端矩阵（Windows / Linux / macOS）执行自动化测试与三端打包（macOS `.app`、Linux `.deb`、Windows `.exe`）；
  - **严禁**在本机 `/private/tmp` 创建数十 GB 的 Debian/Ubuntu 临时 rootfs 树或独立 Go/Swift 编译缓存副本。

## 3. 极速批次执行规范（废除微粒度拆单空转）

- **按里程碑批次（Milestone Batch）连续开发**：
  - 围绕 `docs/specs/001-markstack/plan.md` 中的单个里程碑（`M1`–`M4`），在**同一个分支/工作树**中连续完成全部子项，**严禁每改几行代码就新建 worktree 或拆成碎片 Issue/PR**。
  - 若需向 GitHub 提交 PR，**每个里程碑只建 1 个总括 Issue（Umbrella Issue）**，认领 `/claim` 一次覆盖整批任务，里程碑完成时合并提交 **1 个 PR**。
- **分层验证**：
  - 日常子任务仅跑受影响包的聚焦单测（`< 5 秒`），使用隔离环境变量 `MARKSTACK_RECOVERY_PATH` / `MARKSTACK_SESSION_PATH`，绝不触碰用户真实配置；
  - 里程碑收口时统一跑一次全量 `go test ./... -count=1`、三端构建检查、`node .github/scripts/repository-policy.cjs` 及桌面包打包。
- **磁盘纪律**：直接复用系统共享 `GOCACHE` / `GOMODCACHE`，临时测试二进制用完即删。

## 4. 工程守则与明确冻结项（Hard Stop List）

1. **坚守原文权威契约**：Markdown 编辑继续通过 `Document.EditRange` 与经过验证的源区间补丁回写，保留未触及字节、BOM 与 CRLF/LF，未提交的 IME 预编辑文本不落盘。
2. **冻结 `.mh` 富文本扩编**：`.mh` 引擎维持当前已实现能力与稳定性，**不再为其投入新功能工时**，全部精力集中于 Markdown（Typora 替代核心）。
3. **废除逐像素死锁门禁**：取消“必须与 Typora 固定视口逐像素差分一致才允许开发新功能”的限制；以三端排版清晰美观、阅读与编辑切换不跳行、交互顺滑为验收标准。
4. **锁死 Viva 依赖**：保持当前 `go.mod` 中已验证的 Viva 版本稳定，仅在里程碑边界或确有跨平台阻塞缺陷时按批升级，严禁逐 SHA 追更。未经用户明确授权，不执行 push、merge 或发布操作。
