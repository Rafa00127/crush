# Fork 改动记录（`local/dshlike` 相对 `origin/main`）

这份文档记录本 fork 相对上游 charmbracelet/crush 的**全部偏离**，目的是让上游同步
（见 [FORK_MAINTENANCE.md](FORK_MAINTENANCE.md) 与 `fork-maintenance` skill）时知道
哪些文件是自己改过的、为什么改、解冲突时该保谁。

基线（每次同步后更新这两行）：

- `origin/main` = `b576f95`（merge-base `b576f95`）
- 本地 `local/dshlike` = `3c91a87`，超出上游 **19 个提交**；已提交改动 63 个文件、
  +2809 / −622 行。此外还有一批**未提交**的改动（见第 1 节）。

判读命令：

```bash
git log --oneline origin/main..HEAD          # 已提交的偏离
git diff --stat origin/main..HEAD            # 已提交的规模
git status --short && git diff --stat        # 在途改动
```

---

## 1. 后台作业与长工具调用（未提交，进行中）

这一块是当前最大的本地改动，解决"长命令把整轮对话卡住、出错也看不出来"。

| 改动 | 位置 | 为什么 |
|---|---|---|
| 执行器补 `WaitDelay` | `internal/shell/exec_windows.go`、`exec_unix.go`、`dispatch.go` | 父进程退出后仍抓着 stdout/stderr 的子进程会让 `cmd.Wait` 永不返回，命令明明已失败却显示 running（go.dev/issue/23019）。Windows 上上游从不设该字段，Unix 上 Crush 自己的 handler 也漏了 |
| 共享 `exitStatusFromError` | `internal/shell/exec_handler.go`（新） | 两个平台共用退出码映射，并把 `exec.ErrWaitDelay`（仅退出码 0 时出现）当作成功 |
| `Kill` 加 5s 上限 | `internal/shell/background.go` | 原先是裸 `<-shell.done`，进程收不掉时 `job_kill` 与 ESC 一起冻住 |
| 作业活性数据 | `internal/shell/capped_buffer.go`、`background.go` | 记 `startedAt` / `lastOutputAt` / 累计字节，`Elapsed()`、`IdleFor()`、`OutputWritten()` |
| `job_output` 状态行 | `internal/agent/tools/job_output.go` | `running (elapsed 3m2s, last output 58s ago, 1.2 KB captured)`，把"卡住"和"慢"分开 |
| `job_output` 作业盘点 | 同上 | 省略 `shell_id` 时列出所有作业（running 优先），取代手写 PowerShell 查进程 |
| 重定向输出检测 | `internal/agent/tools/bash.go` | 命令写日志文件时作业永远是 `no output`；用 mvdan 解析器只看**最后一条顶层语句**的重定向，并点名日志文件 |
| 重定向命令 2s 交棒 | 同上（`RedirectedOutputGrace`） | 输出不可能到达我们这边，等满阈值没有意义 |
| 默认交棒阈值 60s → 15s | 同上 + `internal/config/config.go` + `internal/shellconfig/options.go` | 让对话尽早空出来；新增 `option auto-background-after N` / `options.auto_background_after` |
| 交棒文案 | `bash.go`、`bash.md.tpl`、`job_output.md` | 明确告诉模型"已转后台、输出还在、你可以继续聊"，并说明如何回头看结果 |

尚未做：`job_output(wait=true)` 仍无上限（只能 ESC 中断）；Windows 侧没有 Job
Object，`job_kill` 杀不到孙进程；跨会话的进程盘点也还看不到 Crush 之外的残留。

---

## 2. 已提交的改动（19 个提交，按主题）

### 2.1 对齐 DeepSeek Harness 的工具与提示词
`8baae16`、`b2c1e35`、`26170f7`

- 系统提示词、工具描述、上下文注入方式按 DSH 收紧，对照表见
  [DSH_PARITY.md](DSH_PARITY.md)；同时改了 `internal/agent/templates/*`、
  `internal/agent/prompt/prompt.go`、`agent.go` 的上下文 reminder 注入。
- 文件：`internal/agent/agent.go`、`coordinator.go`、`prompts.go`、
  `templates/**`、`internal/agent/tools/*.md*`、`internal/proto/tools.go`。

### 2.2 Provider 侧 web search
`28702cf`、`fb61d22`

- `web_search` 可以改走 provider 提供的搜索端点（DeepSeek `tools/search` 形状），
  `fetch` 子代理也一样；配置在 `options.web_search`（`provider`/`url`/`api_key`/
  `headers`/`count`/`need_content`/`timeout`）。
- 文件：`internal/agent/tools/provider_search.go`（新）、`web_search.go`、
  `agentic_fetch_tool.go`、`internal/config/config.go`、`schema.json`。

### 2.3 MCP：工具 schema 按需加载 + ping 兼容
`32aee62`、`75acba7`、`6df2ce0`

- MCP 工具在超过阈值后只发名称+一句话摘要，模型用 `tool_search` 拉全量 schema；
  配置 `options.tool_search`（`enabled`/`min_tools`/`always_load`/`max_results`）。
- 服务器不实现 ping 时不再把会话判死（`internal/agent/tools/mcp/init.go`）。

### 2.4 Windows / 中文输出
`45e584b`、`11ee1c9`

- 修复 Windows 命令输出的代码页与混合编码问题：`internal/shell/encoding.go`
  （新增/重写）、`DecodeTerminalText*` 在 bash 与 job_output 的格式化里调用。

### 2.5 小改动
| 提交 | 内容 |
|---|---|
| `ff0e2ed` | read 的图片上限 5MB（`tools/view.go`） |
| `e718f0a` | `ls` 类目录列表包含 exe（`internal/fsext/ls.go`） |
| `1fd0f76` | UI 与编辑错误里统一用 `read` 这个工具名 |
| `850e9e9` | 本 fork 不再检查上游新版本（`internal/app/app.go`） |

### 2.6 仓库卫生与文档
`8f85ffc`、`efe1f82`、`26170f7`：忽略 `FORK_MAINTENANCE.md` 与本地截图；
新增 `DSH_PARITY.md`；本文件与 `NOTES.md` 记录本地约定。

---

## 3. 同步上游时的注意点

**最容易冲突的文件**（改动集中且上游活跃）：

```
internal/agent/agent.go            上下文注入、turn 收尾
internal/agent/coordinator.go      工具装配（NewBashTool 参数、tool search）
internal/agent/tools/bash.go       后台作业、重定向检测、阈值
internal/agent/tools/job_output.go 状态行、作业盘点
internal/config/config.go          Options 字段（web_search、tool_search、auto_background_after）
internal/shellconfig/options.go    option 名字表
internal/agent/templates/*         提示词
```

**建议顺序**（与 `fork-maintenance` skill 一致）：`go build ./...` → 改过的包 →
连带包 → 全量测试 → 二进制冒烟。**每次同步后必须补跑**：

```bash
go build ./...
go test ./internal/shell/ ./internal/agent/tools/ ./internal/config/ ./internal/shellconfig/
```

**已知本机失败用例**（与改动无关，`bash` 指向 WSL、拿不到 `C:/` 路径）：

- `TestDispatch_BashShebang`
- `TestDispatch_ShebangPassesExitCode`

## 4. 维护规则

- 改了 fork 行为就顺手更新本文件对应的那一行；不要让它变成历史清单。
- 新增配置项一律走 `option` 名字表 + `Options` 字段 + `schema.json` 三处，缺一处就
  在 release 后暴露成"配置不生效"。
- 未提交的在途改动同步上游前要先落成提交，否则 stash/merge 会把两批改动混在一起。
