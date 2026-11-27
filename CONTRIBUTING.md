# 参与开发

墨栈（MarkStack）是一个原生支持 **Windows、Linux、macOS** 的本地优先 Markdown 工作区，采用**里程碑批次驱动（Milestone-Batch）**的敏捷开发模式。

## 开发与提交流程

1. **按里程碑整批开发**：按照本地 `docs/specs/001-markstack/plan.md` 中的里程碑批次（`M1`–`M4`）组织开发，所有功能必须兼顾 Windows、Linux、macOS 三端兼容（路径、CRLF/LF、快捷键修饰键与构建），不在日常迭代中把小改动拆分为大量零碎 Issue。
2. **远端 PR 与三端 CI 验收**：
   - 每个里程碑建立 **1 个总括 Issue（Umbrella Issue）**，评论 `/claim` 获得机器人分配后，在单一里程碑分支（如 `codex/m1-table-list-editing`）内完成该阶段全部子任务。
   - 收口时提交 1 个汇总 PR，由 GitHub Actions 执行跨平台测试、质量门禁与打包构建。
3. **测试与缓存纪律**：所有测试必须使用临时目录与显式的 `MARKSTACK_*_PATH` 隔离路径，严禁读写用户真实文档或恢复日志；复用系统共享 Go 构建缓存，不在 `/tmp` 堆积独立缓存或系统镜像。

## 本地材料边界

- 私有文档与截图仅保留在本地，不提交 `docs/`、`specs/`、`plans/`、`screenshots/`、`plan*.md`、执行账本或任何截图。
- 产品位图白名单仅限 `assets/markstack-icon.png`。提交前运行 `node .github/scripts/repository-policy.cjs` 校验。

## 验证分级

### 日常子任务验证（秒级）

```sh
go test ./internal/markdownedit ./cmd/markstack -run '<FocusedTest>' -count=1
GOOS=windows GOARCH=amd64 go vet ./internal/... ./cmd/markstack
GOOS=linux GOARCH=amd64 go vet ./internal/... ./cmd/markstack
```

### 里程碑收口门禁（每个里程碑跑一次）

```sh
go test ./... -count=1
go test -tags vivahead ./... -count=1
go vet ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags vivahead ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags vivahead ./...
node .github/scripts/repository-policy.cjs
bash scripts/package.sh --no-sign
```
