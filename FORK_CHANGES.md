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
| `job_output` 取消等待 | 同上 | 删掉 `wait` 参数：作业永远不阻塞模型，否则一个 `wait=true` 就能把整轮对话挂住（排队消息要等 step 边界才能折叠进当前轮） |
| 作业完成提示 | `internal/shell/background.go`、`internal/agent/notify`、`internal/app/app.go`、`internal/ui/model/ui.go` | 只有**真正被交棒**的作业在结束时发一条 `notify.TypeJobFinished`；同步返回的作业不打扰。只用 TUI 提示，不发系统通知 |
| 作业完成弹框 | `internal/ui/dialog/job_notice.go`（新）、`internal/ui/model/ui.go` | 与 **Permission required 同一套外壳**（`Styles.Dialog.View` + 渐变标题 + 两个按钮）：`Tell Crush` / `Dismiss`，`enter` 确认、`←/→`/`tab` 切换、`y`/`n` 快捷；不再用状态栏那行字 |
| 转交时直接带输出 | `internal/ui/model/ui.go` | 确认后发出的是一条 **hidden user 消息**（用户没打字，不能当成他的话回声）且自带结果：`This is the result of background job 00A, which you started earlier … Its output: <…>`，模型不需要再调 `job_output`；超长保留头尾（8000 字符）并提示可再读 |
| yolo 自动转交 | 同上 | yolo/sysadmin 不弹框、自动把同一条带输出的消息发给模型，行为与加提示之前一致 |
| 重定向输出检测 | `internal/agent/tools/bash.go` | 命令写日志文件时作业永远是 `no output`；用 mvdan 解析器只看**最后一条顶层语句**的重定向，并点名日志文件 |
| 重定向命令 2s 交棒 | 同上（`RedirectedOutputGrace`） | 输出不可能到达我们这边，等满阈值没有意义 |
| 默认交棒阈值 60s → 15s | 同上 + `internal/config/config.go` + `internal/shellconfig/options.go` | 让对话尽早空出来；新增 `option auto-background-after N` / `options.auto_background_after` |
| 交棒文案 | `bash.go`、`bash.md.tpl`、`job_output.md` | 明确告诉模型"已转后台、输出还在、你可以继续聊"，并说明如何回头看结果 |

**有意保留的取舍**：Windows 侧不做进程树击杀（没有 Job Object，`job_kill` 只终止
直接子进程）。这是对的——`npm start`、`python -m http.server` 这类脚本的产出物本来
就该活过父进程；连带杀掉会毁掉正常用法。可见性由作业本身提供：`job_output` 不带
`shell_id` 会列出所有作业（running 优先），每行带 elapsed / 最后输出时间 / 已收字节，
模型不用猜"还有没有东西在跑、是不是卡住"。

**有意保留的取舍**：作业缓冲只放内存（每流 1MB、保留 8h / 最多 250 个），不写盘。
写盘会散落一堆日志文件，收益也不对等：结果一旦被读出来，它就作为 tool result（或
转交用的 hidden user 消息）进了会话历史、存在 SQLite 里，重启后仍在；只有**从没被
读过的作业缓冲**会随进程消失。真要跨会话保留，用命令自己 `> log` 重定向即可。

**实测过的边界**（2026-11-13，四组对照，`KillAll` 只 `TerminateProcess` 直接子进程）：

| | 终端留着（只退 Crush） | 关掉终端 |
|---|---|---|
| **孙进程同控制台** | 孙进程**存活**（8897，重启后仍返回 200） | **死**：整个控制台收到 `CTRL_CLOSE_EVENT`（8896 实测） |
| **孙进程 `DETACHED_PROCESS`** | 孙进程**存活**（8898，返回 200） | **仍存活**（8898 那次终端也关了，重启后依然 200——它没有控制台，收不到该事件） |

因此有两句结论，别混为一谈：

1. **只退出 Crush（终端还在）**：杀不杀跟 detached 无关——`KillAll` 只
   `TerminateProcess` 直接子进程，孙进程一律留下。
2. **关掉终端**：这一维才看 detached——同控制台的被控制台关闭事件带走，detached 的
   因为没有控制台而留下来。

退出时的告别屏只有 `Thanks for using Crush!` 与恢复会话的命令，**不提**还有后台作业在跑。

对"起服务器/守护进程"这类用法这是**正确**的；对一次性脚本就是残留。Crush 看不到它们
（作业表里没有），只能靠外部检查（`Get-CimInstance Win32_Process`、端口占用）。

可能待办（低优先）：在退出告别屏加一行 `N background job(s) still running; their child
processes may outlive Crush`，只在有 running 作业时显示。**只做提示、不做进程树强杀**
——强杀会误伤正当用法：例如"帮我把游戏/服务拉起来"，玩的时候顺手关了 Crush，游戏不该
被连带杀掉。

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

## 5. 上游同步计划（2026-11-14 待做）

**目标**：把 `origin/main`（`dd40fd9`，105 个提交，已打 `v0.100.0` / `v0.101.0`）
合进 `local/dshlike`。工作区当前干净，可以先动。

### 5.1 上游这轮改了什么（先读，别猜）

- **主线：子代理（sub-agents）** —— `internal/agent` +6,253、`internal/ui` +4,073：
  detached 派发、中途发消息、命名与配色、transcript/侧边栏/退出对话框里的进度、模型覆盖、
  报告作为独立 message part。
- 其它 feature：`pipe question tools`、`skill toggle`、`CRUSH_DATA_DIR`、
  消息持久化模型显示名、**工具调用耗时显示**（只改 UI：`ui/chat/tools.go`、`ui/common/timer.go`）。
- 值得盯的 fix：`fix(permissions): make yolo mode block dangerous commands without asking`
  （动 `bash.go`，与本地 yolo/sysadmin 语义正面相撞）、
  `fix(agent): stop a cancelled or silent stream instead of hanging the turn`、
  `fix: crush run should not spawn a server if none is running`、
  `fix(ui): preserve CRLF lines in bang mode output`。

### 5.2 冲突面（按 merge-base 行级重叠算出来的）

我们动过 88 个文件，其中 **58 个上游没碰**；双方都动的 30 个里，真重叠的只有这 6 个：

| 重叠 hunk | 文件 | 备注 |
|---|---|---|
| 6 | `internal/agent/prompt/prompt.go` | 提示词拼装 |
| 6 | `internal/agent/coordinator.go` | 上游 53 处（子代理），最痛 |
| 3 | `internal/agent/tools/bash_test.go` | 测试 |
| 3 | `internal/agent/agentic_fetch_tool.go` | fetch 子代理 |
| 3 | `internal/agent/plan_prompt_test.go` | 测试 |
| 2 | `internal/agent/agent.go` | 上游 65 处（子代理），最痛 |

各 1 处、基本是相邻样式改动的：`tools/web_search.go`、`tools/bash.md.tpl`、
`templates/agent_tool.md`、`templates/plan.md.tpl`、`tools/web_fetch.md.tpl`、`.gitignore`。

**零重叠（可自动合）的自家重点**：`internal/shell/*`（上游未动）、
`internal/agent/tools/bash.go`（我们 22 处 vs 上游 5 处，行不重叠）、
`internal/agent/tools/job_output.go`、`internal/ui/model/ui.go`（8 vs 31）、
`internal/config/config.go`（8 vs 6）、`internal/agent/notify`、`internal/ui/dialog/*`。

### 5.3 执行顺序

```bash
cd D:/ProgrammingStuff/Crush/my_fork
git status                       # 必须干净
git branch backup/pre-sync-20261114
git fetch origin
git log --oneline HEAD..origin/main | wc -l      # 应为 105
git merge origin/main            # 用 merge，保留分叉历史
```

解冲突顺序（从易到难，别一上来啃 agent.go）：

1. 各 1 处的琐碎文件（`*.md.tpl`、`.gitignore`、`web_search.go`）——取上游 + 保留本地文案。
2. `bash_test.go` / `plan_prompt_test.go`——两边都是测试，合并用例即可。
3. `agentic_fetch_tool.go`——上游把 fetch 改成子代理；保留本地"provider 侧 web search"路径。
4. `tools/bash.go`——**语义冲突，逐个读**：上游让 yolo 下危险命令仍然被拦，本地有
   yolo/sysadmin 模式与可配置危险命令列表（`blocked_commands`）。原则上取上游的拦截时机，
   叠加本地的 sysadmin 豁免与配置项；`auto_background_after`、重定向检测、交棒文案必须保留。
5. `prompt.go` → `coordinator.go` → `agent.go`——上游子代理重构的核心。合并时保住：
   本地新增的上下文/提示词段落、`tools.NewBashTool` 多出的 `autoBackgroundAfter` 参数、
   `notify.TypeJobFinished` 的发布点。

### 5.4 合并后必须验证

```bash
go build ./...
go vet ./internal/...
go test ./internal/shell/ ./internal/agent/tools/ ./internal/config/ ./internal/shellconfig/ \
        ./internal/ui/... ./internal/app/
```

已知会失败（与改动无关，`bash` 指向 WSL）：`TestDispatch_BashShebang`、
`TestDispatch_ShebangPassesExitCode`。

**功能冒烟**（用本文档的编译部署流程，或直接跑 `crush-dshlike.exe`）：

1. `python -c "import time; time.sleep(30); print('睡好了')"` → 15s 交棒（或配置值），
   期间能插话；完成弹 `Background Job Finished`，Enter 后拿到带输出的消息（normal）；
   yolo 下自动送达且不进聊天记录。
2. 重定向命令（`... > out.log 2>&1`）→ 2s 交棒，回包点名日志文件。
3. `job_output` 不带 `shell_id` → 列出作业；带 `wait` → 参数被忽略、立即返回。
4. 中文输出（WSL/编码）仍正常：`python -c "print('中文测试')"`。
5. 若上游的"工具调用耗时"与本地作业状态行在 UI 上重复，二选一或合并显示。

### 5.5 回滚

```bash
git merge --abort          # 还没提交时
git reset --hard backup/pre-sync-20261114
```

出问题的判定标准：`go build ./...` 不过、或第 5.4 的冒烟有任意一条不满足，先回滚再重来，
不要在坏状态上继续叠改动。
