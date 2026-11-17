# Fork 改动记录（`local/dshlike` 相对 `origin/main`）

这份文档记录本 fork 相对上游 charmbracelet/crush 的**全部偏离**，目的是让上游同步
（见 [FORK_MAINTENANCE.md](FORK_MAINTENANCE.md) 与 `fork-maintenance` skill）时知道
哪些文件是自己改过的、为什么改、解冲突时该保谁。

基线（每次同步后更新这两行）：

- `origin/main` = `fb19c85`（上游已打 `v0.100.0` / `v0.101.0`）
- 本地 `local/dshlike` = `9a75185`，**已合并上游**（原先落后 121 个提交）；
  相对上游仍保留 **40 个本地提交**、87 个文件、+4976 / −717 行。

判读命令：

```bash
git log --oneline origin/main..HEAD          # 已提交的偏离
git diff --stat origin/main..HEAD            # 已提交的规模
git status --short && git diff --stat        # 在途改动
```

---

## 1. 后台作业与长工具调用（已提交）

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
| 作业结果自动送达 | 见"作业完成提示/弹框"两行 | 作业结束时输出随消息送到模型，**模型不需要**去取；提示词明写"不要为等结果轮询 `job_output`" |
| `job_output` 默认不等待 | `internal/agent/tools/job_output.go`、`internal/config/config.go`、`internal/shellconfig/options.go` | `job_output_wait` 默认 **0**（不等）：等结果只会占住 turn。需要时配 15/30s，到点返回 `running` + 已收输出；`0` 时若模型传 `wait=true` 会明确告知"等待已禁用" |
| 重定向输出检测 | `internal/agent/tools/bash.go` | 命令写日志文件时作业永远是 `no output`；用 mvdan 解析器只看**最后一条顶层语句**的重定向，并点名日志文件 |
| 重定向命令 2s 交棒 | 同上（`RedirectedOutputGrace`） | 输出不可能到达我们这边，等满阈值没有意义 |
| 默认交棒阈值 60s → 15s | 同上 + `internal/config/config.go` + `internal/shellconfig/options.go` | 让对话尽早空出来；新增 `option auto-background-after N` / `options.auto_background_after` |
| 外部 bash 执行器 | `internal/shell/shell.go`、`background.go`、`internal/config/config.go`、`internal/shellconfig/options.go` | `options.bash_path` 命中时命令交给那个 bash（`-c`）执行，替代内建解释器：`WaitDelay`、退出码、流式输出契约不变。Windows 上这是让 msys2 可用的唯一方式（PATH 上的 `bash` 通常是 WSL）。`options.shell_env` 提供额外环境变量，值支持 `$VAR`/`%VAR%` 展开（`PATH=<bash 所在目录>;$PATH`） |
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

**关于"模型老去问 `job_output`"**（2026-11-14 结论）：根因不是缺等待手段，而是提示词
没说清"结果会自动送到你手上"。模型以为必须主动取，于是 1 秒一次地刷（实测同一 job 出现
`elapsed 19s/21s/22s/23s` 四条记录）。现在两处一起改：`bash.md.tpl` 与 `job_output.md`
明确"作业结束时输出自动送达，**不要为等结果轮询**，`job_output` 只用于查看进度或用户问起"，
同时 `job_output_wait` 默认 0（配置可开）。有界等待仍保留：需要"下一步马上要用结果"时，
`option job-output-wait 15` 即可，且永远不会无限挂。

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

## 5. 上游同步（2026-11-17 已完成）

`origin/main`（`fb19c85`，121 个提交）已合入，提交 `bcd635e`；紧随其后两个小提交
（`test:` 修正断言、`style:` gofumpt）。结论与要点：

- **冲突只有 14 个文件**，且都是预期内的那几个：模板文案、工具清单、`bash.go` 的签名、
  `coordinator.go` 的工具装配、`prompt.go` 的 Option 集合、`ui/chat/file.go` 的工具名。
- **我方功能全部保住**：`autoBackgroundAfter` 交棒、`job_output` 有限等待/盘点/默认不等、
  `TypeJobFinished` 弹框与 hidden 转交、provider 侧 web search、`tool_search`、strip 构建。
- **一处语义按上游改了**：`yolo` 模式现在**会拦下危险命令**（不弹框，直接回
  `yoloBlockedMessage` 并告诉用户怎么放行）。本地原来的 yolo 是"不弹也不拦"，放行全靠
  `sysadmin`。保留上游行为，是因为它更安全、也是上游接下来的方向；`sysadmin` 仍是
  "什么都放行"的口子。若想退回旧行为，把 `permission.PermissionMode.RefusesDangerous()`
  改成恒 `false` 即可（同时要改回 `TestBashTool_YoloModeBlocksDangerousCommandsWithoutPrompting`）。
- **上游新增子代理系统**（与本地的作业系统并存，互不影响）：
  - 工具：`agent_dispatch`（起子代理，立即返回）、`agent_send`（中途发消息/换模型）、
    `agent_status`、`agent_stop`；阻塞式 `agent` 工具保留。
  - 每个子代理有**自己的子会话**（`CreateAgentToolSessionID`），UI 把它的工具调用嵌套在父
    会话那个 tool call 下面；名字是 `label` 前缀 + 名单里的名（`betty-date-impl`）。
  - **报告以独立 message part（`<subagent-report>`）投递**，唤醒父代理的新 turn。
  - 阻塞式子代理调用超过 `subAgentBackgroundAfter = 60s` 会被 detach 到后台——注意这是
    **给子代理的**阈值，和本地的 `auto_background_after`（给 bash 命令的，默认 15s）是
    两套东西，各管各的。
- 合并后测试：`go build ./...`、`go vet`、gofumpt 全过；`go test ./...` 只剩两个既有
  WSL/bash 用例失败（`TestDispatch_BashShebang`、`TestDispatch_ShebangPassesExitCode`）。

### 5.x 原计划（留档）

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
cd <fork 仓库目录>
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

## 6. 极简模式（`--mode mini`）与外部 bash（2026-11-17）

目标：对齐 DSH 的 minimal preset（一个 shell + 一句提示词），把固定 token 成本压到最低。

### 6.1 开关与组成

`crush --mode mini`，或 `crush --mode mini-mcp`（`--mode` 只接受 `""` / `"mini"` / `"mini-mcp"`，其他值直接报错；可叠加 `-y`/`-yy`/`--channels`）。

默认模式也可以写进配置：`options.mode`（全局或项目均可，值同上）。优先级：显式 `--mode` > `options.mode` > full；`--mode ""` 可显式回到完整模式。本机的全局配置现在是 `"mode": "mini-mcp"`。

| 层 | mini | mini-mcp |
|---|---|---|
| system | **一句**：`You are a helpful software engineer assistant.`（`templates/mini.md.tpl`） | 同左 |
| 会话上下文 | 一条 `<system-reminder>`，**只带 cwd**（`templates/mini_context.md.tpl`） | 同左 |
| 工具 | `bash` + `read_image` + `fetch` + `web_search` + `job_kill`（`coordinator.miniToolNames`）；MCP 一个不挂 | 同左 **+ `read`/`edit`/`write`/`job_output`**（`miniMCPToolNames`：diff 预览、编辑后 LSP 诊断、省 token 的带行号读取、可回读的后台作业——shell 都替代不了）+ **所有 MCP 工具**（`tool_search` 按你配置的阈值把它们 schema 延后）；MCP server 正常启动。`grep`/`glob`/`ls`/`todos` 仍不加：shell 做得一样好 |
| bash 描述 | `tools/bash_mini.md`（约 700 字符）。完整版约 11.5KB，其中危险命令清单就占 ~1.7KB | 同左 |
| 上下文提醒 | 不进 AGENTS.md / 技能表（与 DSH minimal 一致） | 同左 |
| 权限 | 不改变，仍由 `-y` / `-yy` / 配置决定 | 同左 |

两个模式都不碰 plan agent（它的列表里没有 bash，裁了就无工具可用），子 agent 也不受影响。

### 6.2 新增/改动的文件

- `internal/agent/templates/mini.md.tpl`、`mini_context.md.tpl`（新）
- `internal/agent/prompts.go`：`miniPrompt()`，带极短 reminder 模板
- `internal/agent/coordinator.go`：`miniMode()`、`miniKeepsMCP()`、`miniToolNames`、`effectiveToolNames` 的 mini 分支（只裁 coder）、`buildTools` 的 MCP 开关、system prompt 选择
- `internal/app/app.go`：`--mode mini` 下不 `ArmInit`、不 `mcp.Initialize`（mini-mcp 照常启动）
- `internal/agent/agent.go`：todos 提醒只在工具集真有 `todos` 时追加
- `internal/config/store.go`：`RuntimeOverrides.Mode`（`ModeFull` / `ModeMini` / `ModeMiniMCP`）+ `MiniMode()`
- `internal/config/config.go`：`Options.Mode`（`options.mode` 默认模式）+ `internal/shellconfig/options.go` 的 `option mode`
- `internal/cmd/root.go`：`--mode` flag、取值校验、`--mode` > `options.mode` 的优先级
- `internal/agent/tools/bash_mini.md`（新）与 `NewBashTool(..., miniMode)`：mini 用短描述
- `internal/agent/tools/read_image.go` + `read_image.md`（新）：只吃图片的读取工具（PNG/JPEG/WebP/GIF、5MB、内容嗅探 MIME、工作区外仍走权限、模型不支持图片时明确提示），只挂在 mini
- 测试：`internal/agent/mini_mode_test.go`、`internal/cmd/mode_flag_test.go`、`internal/agent/tools/read_image_test.go`、`TestBashToolMiniDescriptionIsShort`

### 6.3 外部 bash（与 mini 独立，普通模式也可用）

- `options.bash_path`：命中时 shell 层用 `<bash> -c "<command>"` 执行，替代内建 mvdan 解释器；`WaitDelay`、退出码、流式输出、作业模型全部不变。没设就退回解释器。
- `options.shell_env`：`KEY=VALUE` 列表，值支持 `$VAR` / `%VAR%`；**`PATH` 是前置语义**（自动拼上继承的 PATH，不需要写 `$PATH`）；覆盖同名变量时按大小写不敏感剔除继承项（Windows 拼作 `Path`）。
- 本机实际取值（msys 路径、PortableGit、`HOME`、`/tmp` 的 fstab 挂载）记在 **`LOCAL_SETUP.md`**——那个文件与
  `FORK_MAINTENANCE.md` 一样**不进 git**（里面有机器路径）。这里只留可复用的结论：
  - `bash_path` 写 bash 二进制的**绝对路径**。PATH 上的 `bash` 在 Windows 上通常是 WSL，路径是 `/mnt/c/...`，
    和 msys 的 `/c/...` 语义不同，不能混用。
  - `shell_env` 的 `PATH` 是**前置**语义，顺序有讲究：编译器所在目录放最前；想让 `git` 用 Windows 版就把它的
    `cmd` 目录放在 msys 之前（msys 自带 git 没有 credential helper，push 会卡在用户名提示），但**只放 `cmd`**
    （里面只有 `git`/`gitk`/`scalar`/`tig`），别放 `mingw64/bin`，否则 `curl`/`openssl`/`xz` 一大票都被前置；
    `usr/bin` 放最后保住 coreutils 与 `sh`（hooks 用的是 `sh`）。
  - `HOME` 要显式指到 Windows 用户目录：msys bash 默认是 `HOME=/home/<user>`，git 因此读不到 `.gitconfig`
    （提交报身份未知、push 卡在用户名提示），`${HOME}/.config/crush/hooks/*.sh` 这类 hook 路径也会指错。
  - msys 的 `/tmp` 默认在安装目录下，与 `%TEMP%` 是两个地方；在 `/etc/fstab` 里把它挂到系统 temp，
    两个 shell 的临时文件就互通了，旧的临时目录可以留着当历史。
- 以上配置写在**全局**配置里而不是仓库里：换工作目录也生效，也不会跟着仓库同步被覆盖。

### 6.3.1 REA 只挂 28 个工具（`mcp.<server>.enabled_tools`）

REA 注册 255 个工具（6.9.0；6.7 时是 207）；全挂时即使 `tool_search` 把 schema 延后、只发“名字 + 首句”，固定开销也有约 12k token/请求（见 6.4 表）。所以全局 `~/.config/crush/crush.json` 里给了 `mcp.rea.enabled_tools` 一个**允许清单**，只留本机 46 个 session DB 里实际用过的 28 个（并集：Galgame_AI 22、MakePortableAppsAndGameCheat 15、reverse 7、恶之女干部满月之月 11、my_fork 2、videoFix 2），开销降到约 1.6k。

- **代码不用改**：`MCPConfig.EnabledTools` 早就存在，`tools.go:filterTools` 先按它过滤，再减 `disabled_tools`；`mcpConfigEqual` 也已把它算进“要不要重启 client”的比较。
- **为什么用允许清单而不是禁用清单**：跨配置文件的数组是**追加**（见 6.3 的 shell_env）。允许清单意味着“全局收紧、某项目追加放开”天然可用；禁用清单做不到——项目无法取消全局的禁用项。
- **按项目加**：在项目的 `.crush.json` 里写 `"mcp": {"rea": {"enabled_tools": ["额外工具名"]}}`，会叠加在全局 28 个之上。短名（`xrefs`），不是 `mcp__rea__xrefs`。
- **副作用**：REA 升级新增的工具默认不可见，需要时手动加。工具真实名字用 `node ~/.config/crush/skills/rea-token-budget/scripts/list-tools.mjs [关键词]` 枚举（MCP 握手拿到的表），说明见同名 skill。
- **2026-11-17 升级到 6.9.0**：工具数 207 → **255**，28 个白名单名字全部仍存在（升级后实测）。前置条件是把本机 Node 从 24.19.0 换到 **24.22.1**（REA 6.8+ 要求 `^22.23.4 || ^24.22.1 || >=26.12.1`）。换法是官方 win-x64 zip 覆盖绿色版 Node 目录（该目录的 ACL 只给普通用户 ReadAndExecute，需要在管理员控制台里改名+替换）；旧目录留作 `NodeJS.bak-<时间戳>`。npm 的全局前缀原本写在旧版 `node_modules/npm/npmrc`（`prefix=${APPDATA}\npm`），换本体后改用 `~/.npmrc` 里的 `prefix=%APPDATA%\npm` 保持原行为。
- 6.9.0 的默认值变化会影响调用写法：`analyze_function` 默认只回 retained summary（要全文传 `detail: "complete"`），MCP 输入 schema 默认紧凑，`close_binary` 保留记录（释放用 `reset_investigation`），单次数组/对象元素上限 1024。


### 6.4 与 DSH minimal 的对照

| | DSH minimal | 本 fork mini |
|---|---|---|
| system | 一句 | 一句（相同） |
| 会话上下文 | `<system-reminder>` 带 cwd | 相同 |
| 工具 | `bash` + `read_image` + `working_directory` | `bash` + `read_image` + `fetch` + `web_search` + `job_kill`（cwd 由配置决定；联网给的是工具而不是放行 curl；job_kill 用来停自己起的后台作业） |
| bash 描述 | ~595 字符 | ~700 字符 |
| shell 持久性 | 持久（cd/export 跨调用） | **一次性**（每次新进程；提示词里明说用 `&&`） |
| 固定 token（单请求） | 未测 | mini ≈ 1.2k；mini-mcp ≈ 8k；full ≈ 20k（详下） |

**固定 token 预算（单位 token/请求；内置工具按 `.md`/`.md.tpl` 实测描述字节 ÷4，再按 schema 约 +60% 估；pwsh 与 MCP instructions 是估值，无法直接测请求）**

| 组成 | mini | mini-mcp | full |
|---|---|---|---|
| system prompt（含 `<mcp-instructions>`） | 0.05k（47 B，无 MCP） | ~0.5k（一句 + pwsh/REA 两段 instructions） | ~2k（`coder.md.tpl` 5.3KB 渲染后 ~1.4k + instructions ~0.5k） |
| 会话上下文 | 0.06k（只有 cwd） | 0.06k | ~4k（AGENTS.md 11.9KB ≈ 3k + 12 个技能表 ≈ 0.8k + env/git） |
| 内置工具 | 5 个 ≈ 1.1k（描述 2.5KB） | 9 个 ≈ 2.2k（描述 5.9KB） | 35 个 ≈ 8.5k（描述 26.5KB，含 bash 渲染后的 11.5KB） |
| MCP | 0 | ~5.3k（pwsh 5 个 schema 2.35k + REA 28 个 stub 1.2k + 两段 instructions 1.75k） | ~5.3k |
| **合计** | **≈ 1.2k** | **≈ 8k** | **≈ 20k** |

MCP 那 ~5.3k 的构成（全部按 MCP 握手 `tools/list` 实测，不是估的）：REA **白名单 28 个**被 `tool_search` 压成“名字 + 首句 ≤200 字符 + `[params: tool_search]`”，合计 **4794 B ≈ 1.2k**（每个约 171 B ≈ 43 token）；REA 的 instructions **2604 B ≈ 0.65k**；pwsh 因为 `tool_search.always_load` 给完整 schema，**12 个共 18220 B ≈ 4.55k**（`execute_command` 一个就 4185 B），关掉 6 个远端控制 + `get_current_location` 后剩 5 个 **9396 B ≈ 2.35k**；pwsh 的 instructions 另有 **4378 B ≈ 1.1k**（它连同被关掉的远端控制说明一起发，工具关了文字还在）。

> 早先版本的表写 full ≈ 28k / mini-mcp ≈ 14k，那是 REA **203 个工具全挂**时的数（stub 就 12k）。白名单缩到 28 个、pwsh 裁到 5 个之后，MCP 占 5.3k，两行都要按上表重算。想再压：把 `pwsh` 从 `always_load` 拿掉（省 ≈2.35k，代价是每次用前多一次 `tool_search`），或继续裁 REA 白名单（每裁 10 个省约 0.43k）。

这台机器上这些配置项的**当前取值与验证命令**记在 [LOCAL_SETUP.md](LOCAL_SETUP.md)（AGENTS.md 里已引用）。


### 6.5 明确不做的：持久 shell

评估过，放弃。难点不在"起一个长期 bash"，而在：哨兵协议判断命令结束（命令自己打印同样字符串会误判）、stdout/stderr 双管道交错顺序丢失、不能包 subshell（包了 `cd`/`export` 就没了）、超时只能杀整个 shell（要保状态得 `set -m` + 杀进程组）、以及与我们"一个工具调用 = 一个进程 = 一个作业"的模型冲突（后台命令仍需单开进程）。估计 3–5 天，收益（跨调用 `cd`/`export`）不足以抵消。

### 6.6 缓存注意

切模式、切模型、压缩都会让前缀缓存整体失效（改的是前缀最前面的 system/tools）。mini 内部稳定；因此**任何每轮变化的东西**（当前时间、实时 git 状态、作业摘要）都不能进 system prompt / context reminder / 工具描述，只能放在历史尾部或以工具结果形式送达。

具体到“同一个 session 先 `mini-mcp` 后 `full`（换 `--mode` 重启并 `-s` 恢复）”：**缓存全掉**，不是掉一部分。缓存匹配的是最长公共前缀，而 system prompt 与工具列表在请求最前面，第一个字节就不同了，所以连历史一起按未命中重算；那一次请求按全价（+ 缓存写入价）付整段前缀 + 全部历史，之后按新前缀重新缓存。切回 `mini-mcp` 若旧缓存还没过期（DeepSeek 约几分钟到几小时），能命中旧的；过期了同样重算。所以别在长会话里反复切模式，一次切换的代价 ≈ 该会话已有上下文的全部输入价。

### 6.7 提示词卫生：只说存在的工具

- **MCP 在 mini 下完全不启动**（`app.go`：`miniMode` 为真时不 `mcp.ArmInit`、不 `mcp.Initialize`）：少几个 node/python 进程的启动时间，也让 MCP server 的 `instructions` 不会出现在一个用不到它们的 prompt 里。不 `ArmInit` 时 `WaitForInit`/`WaitForInitBudget` 立即返回，首轮不会被卡。
- **todos 提醒按工具门控**：那段“todo list 为空、请用 `todos` 工具”的提醒原本对每个非子 agent 都追加，而 mini 没有 `todos`；现在只在工具集里真有 `todos` 时追加。
- 没有额外加“instructions 按工具集过滤”的逻辑：mini 根本不连 MCP 后就没必要了，少一处分叉。
