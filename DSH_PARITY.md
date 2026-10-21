# Crush → DSH 环境对齐清单

目标：让 DeepSeek 模型（`deepseek-v4-flash` / `deepseek-flash` 一档）在 Crush 里看到的环境尽量接近
DeepSeek Harness（DSH）。只列**模型可见**的差异，不涉及 TUI、存储、权限模型。

> 范围原则：**差异清单不等于待办清单**。只有能指出具体失败、或用数据说明成本、
> 或改动小于 20 行且无行为风险时才改；仅仅“和 DSH 不一样”不改。
> 实际范围以第七节为准。

基线证据：

- DSH 渲染后的系统提示：`snapshots/web/fresh-round-trip/system-prompt.expected.md`（35 行，894 词）
- DSH 默认工具 schema：`snapshots/web/fresh-round-trip/tool-schemas.expected.json`（31 个）
- DSH 提示词拼装顺序：`packages/core/system-prompt/src/index.ts:131-165`

## 一、模型契约（工具名 / schema / 结果格式）

> 下表的“优先级”只表示与 DSH 的对齐度，不代表都要实施。是否改见第七节。

| 优先级 | 改动点 | Crush 现状 | DSH 做法 | 对小肥鱼的影响 | 工作量 | 位置 |
|---|---|---|---|---|---|---|
| P0 | 读工具改名与参数 | `view`；`offset` **0-based**；默认 **200 行** | `read`；`offset` **1-based**；默认 **2000 行** | 工具名不在训练分布内；大文件要读 8 次；offset 系统性错一行 | S | `internal/agent/tools/view.go:49,75-77` |
| P0 | grep 结果格式 | 自定义 `file:` + `  Line N, Char M: text`；上限 100；无上下文 | rg `--json` → 标准 `path:line:text`；上限 250；可带上下文 | 解析成本高，匹配后还要再 view 一遍 | M | `tools/grep.go:158-179`、`tools/grep.md.tpl` |
| P0 | bash 结果标记 | 只有失败时追加 `Exit code N`；成功结果尾部带 `<cwd>` | 每条结果都带 `[exit code: N]`；无 cwd 噪音 | 模型可能漏掉静默失败的构建/测试 | S | `tools/bash.go:377-410` |
| P0 | 系统提示瘦身 | **3290 词 / 21.5KB**，且有三处自相矛盾 | 约 **870 词**，规则随工具走 | 小模型更容易丢中段指令、照错规则执行 | M | `internal/agent/templates/coder.md.tpl` |
| P0 | web 工具形态 | 主 agent 没有 `web_search`，只有 `agentic_fetch` 子代理；A 协议才用服务端搜索，否则 DDG 爬取 | 主 agent 直接有 `web_search`/`web_fetch`，后端可插拔 | 搜索多一层子代理开销；换 OAI 兼容路由行为退化 | M/L | `coordinator.go:884-928`、`agentic_fetch_tool.go:127-210` |
| P1 | 上下文注入位置 | AGENTS.md、技能目录、日期、git status 全进系统提示 | AGENTS.md/技能作为 user 角色 reminder；cwd/沙箱作为 runtime context | 系统前缀随上下文变化，缓存失效；中段干扰 | M | `internal/agent/prompt/prompt.go`、`coder.md.tpl:371-434` |
| P1 | 技能加载方式 | `view` 读 `crush://skills/...` 虚拟路径 | 专用 `skill` 工具 | 与 CC/DSH 习惯不一致，模型不会主动加载 | S | `tools/view.go:443-487`、`coordinator.go:885` |
| P1 | LSP 工具粒度 | 8 个独立工具（`lsp_symbols`、`lsp_definition`…） | 1 个 `lsp` 工具 + operation 枚举 | schema 噪音，模型容易选错操作 | M | `tools/lsp_*.go` |
| P1 | MCP 工具命名 | `mcp_<server>_<tool>`（单下划线） | `mcp__<server>__<tool>`（双下划线） | 与 CC/DSH 不一致，易与本地工具名混淆 | S | `tools/mcp-tools.go:58-60` |
| P2 | `question` 工具形状 | 643 词描述；`type`/`choices`/`confirm_*` 自定义字段 | `questions[]` + `options[]` + `multi_select` | 描述占 token；模型常填错字段 | M（UI 联动） | `tools/question.go:21-65`、`tools/question.md` |
| P2 | 子代理能力 | `agent` 只有 `glob/grep/ls/view`（只读搜索） | `subagent` / `subagent_fork`，完整工具、独立上下文 | 大任务无法并行，主上下文被撑爆 | M | `templates/agent_tool.md`、`coordinator.go:851` |
| P2 | 压缩提示 | 明确写“**No limit**，宁多勿少” | 有上限（CC 约 20k tokens；DSH 走自己的 compaction 契约） | 压缩后可能仍然超窗，触发再次压缩 | S | `templates/summary.md:48` |
| P2 | 低频工具常驻 | `download`、`sourcegraph`、`crush_info`、`crush_logs` 等默认暴露 | 按部署组合，非核心工具不进默认集 | 固定 schema 开销，模型分心 | S | `coordinator.go:884-905` |

## 二、已经一致，不要改

| 项 | 说明 |
|---|---|
| todos 语义 | Crush 的 `todos` 本来就是**整表替换**（`TodosParams{Todos []TodoItem}`），与 DSH `todo_write`、CC `TodoWrite` 一致 |
| 读后写校验 | `edit`/`write` 都有 filetracker 硬校验（`edit.go:283`、`write.go:75`），与 DSH 的 fs-observation-policy 一致 |
| edit 唯一匹配 | 默认要求 `old_string` 唯一；Crush 额外做了空白容错（`edit_whitespace.go`），建议保留为兜底，但默认仍按严格匹配写 |
| 权限模型 | 比 DSH 更细，不是性能瓶颈，不在本轮范围内 |

## 三、建议批次

| 批次 | 内容 | 预期收益 |
|---|---|---|
| A（先做） | 读工具改名 + 1-based + 2000 行；grep 标准输出；bash exit code；系统提示砍到约 900 词并删矛盾 | 改动小、直接对齐训练分布，收益最大 |
| B | web 工具提到主 agent + 后端可插拔（`/v1/tools/search`、辅助 Anthropic Messages、DDG 兜底） | 去掉子代理那层开销，OAI 兼容与 A 协议行为一致 |
| C | 上下文移出系统提示；加 `skill` 工具；合并 LSP 工具；MCP 双下划线命名 | 缓存稳定、schema 更接近 DSH |
| D | question schema、子代理能力、压缩上限、低频工具按需加载 | 长期维护性 |

## 四、验收方式

1. 同一任务在 Crush（改动前/后）与 DSH 各跑一遍，记录：总 tool calls、`read` 调用次数、失败 edit 次数、最终 diff。
2. 用 DSH 的快照做 golden：`system-prompt.expected.md` 与 `tool-schemas.expected.json`，逐项对照工具名、参数名、默认值。
3. 至少覆盖三类任务：单文件修改、跨文件重构、需要搜索的任务（grep + web）。
4. 每批改动后运行 `go test ./internal/agent/...`，涉及 UI 时再跑 `go test ./internal/ui/...`。

## 五、明确不建议照搬

- 不要为了“像 DSH”去掉 LSP：DSH 的 LSP 默认不在 preset 里，但 Crush 的 LSP 工具是实际优势，合并成一个工具即可。
- 不要把 `edit` 的空白容错删掉：它是弱模型的容错网，只要默认提示仍要求精确匹配即可。
- 不要照抄 DSH 的 60s web 搜索超时到所有环境：这是 DeepSeek 辅助模型请求的预算，本地 DDG 兜底不需要。

## 六、复杂度与上游维护评估

前提：`local/websearch` 已有 1 个 websearch 提交（`28702cf`），`local/dshlike` 从同一提交拉出。
本地是浅克隆（561 提交），下面的 churn 数字只是下限。

| 改动 | 代码量 | 实现风险 | 与上游冲突 | 维护成本 | 结论 |
|---|---|---|---|---|---|
| 模型可见名别名层（`view`→`read`） | 150–250 行，新增文件为主 | 中 | 低 | 低 | 做 |
| `read` 1-based + 2000 行 | 30–50 行 | 低 | 低 | 低 | 做 |
| grep 标准输出 + context/head_limit | 150–250 行 | 中 | 中 | 中 | 做 |
| bash `[exit code: N]` | 约 15 行 | 低 | 低 | 低 | 做 |
| 提示词瘦身（overlay 模板） | 新模板 + 约 50 行接线 | 中（需实测） | 低 | 中 | 做 |
| 压缩上限 | 约 5 行 | 低 | 低 | 低 | 做 |
| web 工具一等化 | 500–800 行（新包/新文件） | 中高 | 低到中 | 中 | 做（已有基础） |
| `skill` 工具 | 约 150 行新文件 | 低 | 低 | 低 | 可做 |
| MCP 双下划线 | 约 5 行 + 兼容映射 | 低 | 低 | 低 | 可做 |
| 上下文移到 user 消息 | 300–500 行（`agent.go` + UI） | 中高 | 高 | 高 | 暂不做 |
| LSP 合并为单工具 | 400–600 行（8 工具 + UI + 权限） | 高 | 高 | 高 | 不做 |
| `question` 改造 | 200–400 行（UI 联动） | 中 | 中 | 中 | 不做 |
| 子代理放开完整工具 | 150–250 行 | 中（权限） | 中 | 中 | 后置 |

整体判断：P0 最小集约 6–9 个工作日；加上 web 工具的一轮约 12–18 个工作日；
LSP 合并、上下文迁移、question 改造不建议投入。

### 三条降低维护成本的架构决定

1. **名字用别名层，不做全局重命名**。新增一个 `aliasTool` 包装 `fantasy.AgentTool`：
   `Info().Name` 返回暴露名，`Run` 里把 `ToolCall.Name` 改回内部名再委托。
   内部常量、proto、UI、测试、旧会话都不用动，上游改动不会撞车。
2. **提示词用 overlay 文件**（如 `coder.dshlike.md.tpl`），不动上游 `coder.md.tpl`，
   由配置/agent 选择。代价是上游提示词的修复要人工挑选，但不会每次 merge 都冲突。
3. **web/skill 放在新文件里**，`coordinator.go` 只加注册行；避免改 `agent.go` 的消息组装。

### 跟 main 更新的流程

- 每周 `git fetch origin --tags && git merge origin/main`，不要攒大合并。
- 合并后固定跑：`go build ./...`、`go test ./internal/agent/... ./internal/proto/...`，
  再加一个 golden 测试（系统提示 + 工具 schema 快照），防止上游改动悄悄破坏对齐。
- 冲突热点（可见历史条数）：`coordinator.go` 29、`agent.go` 23、`bash.go` 5、`prompt.go` 4。
  用上面的架构可以把冲突限制在 `coordinator.go` 的几行注册代码。
- `local/websearch` 保持不动作为回退点；`local/dshlike` 只做增量提交，一批一个 commit。

## 七、按证据收敛后的范围（以此为准）

> 数据说明：本项目 `.crush/crush.db` 目前只有 3 个会话（679 条消息、430 次工具调用），
> 且都是 2026-10-21 这次分析任务本身产生的，**不能代表日常使用**。
> 因此下面只保留“客观冲突”和“零风险小改”，其余先观察。

### 7.1 建议改（改动小、可逆）

| 项 | 依据 | 改动 |
|---|---|---|
| 提示词中的两处硬冲突 | `coder.md.tpl:324` 让用 `&` 放后台，`bash.md.tpl:34` 明确禁止；`coder.md.tpl:20` 限制读取，`:72` 又要求编辑前读整个文件 | 改 2 处，约 5 行 |
| 系统提示瘦身 | 当前 3246 词；DSH standard 894 词（核心约 500）。Crush 里 `editing_files` 402 词 + `whitespace` 148 词在讲同一件事 | 目标 600–800 词，细节下放到工具描述 |
| 压缩摘要上限 | `templates/summary.md:48` 写“No limit，宁多勿少” | 加上限，约 5 行 |
| bash 统一 exit code（可选） | 现在只有失败时才有 `Exit code N`，静默失败容易被当成成功 | 约 15 行 |

### 7.2 先量数据再决定（观察项）

| 项 | 先量什么 | 判据 |
|---|---|---|
| `view` 默认 200 行 | 每个任务 `view` 调用数、省略 `limit` 的比例 | 若省略 `limit` 时经常读不全，再调默认值 |
| `view` 改名 / 1-based | `offset=1` 的调用是否出现错位或重复读 | 当前只有 4/204 次传 1，收益不明确 |
| grep 输出 / 上下文 | grep 之后紧接 `view` 同一文件的比例 | 比例高才加 `context` |
| web 工具形态 | `agentic_fetch` 调用率、失败率 | 当前 3 个会话里 0 次，且 A 协议可用 |
| 子代理工具范围 | `agent` 调用次数与失败原因 | 没有失败证据就不放开 |

### 7.3 明确不做

| 项 | 原因 |
|---|---|
| 专用 `skill` 工具 | 现有 `view crush://...` 实测正常 |
| MCP 双下划线命名 | 纯命名差异，没有失败案例 |
| LSP 合并为单工具 | 改动大、UI/权限联动，收益不明确 |
| `question` schema 改造 | UI 联动，当前工作正常 |
| 上下文迁移到 user 消息 | 要改 `agent.go` 消息组装和 UI，收益低于维护成本 |
| 工具全局重命名 | `view` 使用正常，改名会牵动 proto/UI/权限/旧会话 |

### 7.4 怎么量

遍历各项目的 `.crush/crush.db`，统计：工具调用名分布、`view` 的 `offset`/`limit`、
`edit`/`write` 错误数、`agentic_fetch` 次数、`is_summary_message` 数量。
跑一周再回来决定 7.2 里的项。

## 八、DSH 的提示词模式，以及瘦身目标

之前说的“约 870 词”是 **web app 的 standard preset（native 工具模式）**，
不是极简模式。DSH 实际至少有这些组合：

| 模式 | 系统提示 | 工具数 | 说明 |
|---|---|---|---|
| `minimal` preset | **7 词**：`You are a helpful software engineer assistant.` | 3（bash/pwsh、read_image、working_directory） | persona 设 `complete: true`，关闭 runtime context；fs 的 read/write/edit 全关；靠模型自己的工具先验 |
| `standard` preset | **894 词 / 5.6KB** | 31 | 日常编码模式；每个工具一段说明 |
| `ptc` preset | **5499 词 / 39KB** | code mode（`run_code` + 生成 SDK） | 提示词大头是自动生成的 TS/Python SDK 声明，不是给人看的规则 |
| `headless` bundle | base 工具集，去掉 Web GUI 专属段落 | 标准 | 一次性 `dsh --profile headless "<task>"` |
| `sdk-minimal` bundle | 7 词 persona，`includeHarnessIdentity: false` | 2–3 | SDK 场景的极简组合 |

standard 的 894 词可以再拆：

| 组成 | 约词数 |
|---|---|
| identity + persona | 15 |
| `@` 文件引用规则 | 70 |
| 每个工具的引导（bash/read/write/edit/glob/grep/jobs/web/goal/workflow/subagent） | 380 |
| 交付物/文件链接策略 | 200 |
| harness checkout + Web GUI 两段 | 190 |

也就是说，**CLI 场景的核心大约 500 词**；另外约 390 词是 Web 产品专属。

minimal 模式能只有 7 词，前提是：所有规则都在工具描述里（persistent bash 的 description 本身就有 7 条），
而且不启用 runtime context、不启用 fs 工具。它不是“通用日常模式”，更像是给模型做基准测试的裸环境。

### Crush 瘦身的目标

当前 `coder.md.tpl` 共 3246 词，按 section 拆：

| section | 词数 | 问题 |
|---|---|---|
| `critical_rules` | 426 | 与 workflow/decision_making/editing/testing 大量重复 |
| `editing_files` | 402 | 与 `whitespace_and_exact_matching`（148）讲同一件事 |
| `decision_making` | 274 | 与 `workflow`/`proactiveness` 重复 |
| `workflow` | 261 | 部分规则应进工具描述 |
| `skills_usage` | 248 | 规则本身没错，但太长 |
| `communication_style` | 199 | 与 `final_answers`（188）重复 |
| `task_completion` | 187 | 与 workflow/proactiveness 重复 |
| `error_handling` | 168 | 部分与 edit 工具的错误处理重复 |
| 其余（tool_usage、code_conventions、testing 等） | 约 700 | 可压缩 |

建议目标：**600–800 词**，不是 7 词。做法是“搬家”而不是删规则：

| 现在的位置 | 搬到哪里 |
|---|---|
| `editing_files` + `whitespace_and_exact_matching`（550 词） | `edit.md` / `multiedit.md` / `write.md` |
| `error_handling` 的 edit 部分 | `edit.md` 的失败提示 |
| `workflow` + `task_completion` + `proactiveness` | 合并成一段 120 词左右 |
| `communication_style` + `final_answers` | 合并成一段 80 词左右 |
| `critical_rules` 里与工具相关的条目 | 对应工具描述 |

另外可选加一个 `minimal` agent preset（7–50 词 + 只用 bash/view/edit），用于对比测试，
但不要设为默认。

## 九、瘦身到 standard 风格后，如何不破坏 Crush 工具

结论：**能兼容，但要做的是“搬家 + 重写工具路由”，不是把 DSH 的提示词抄过来。**
照抄会在这几处直接坏掉：

| DSH 的假设 | Crush 的实际 | 照抄的后果 |
|---|---|---|
| `read`，1-based，默认 2000 行 | `view`，**0-based**，默认 200 行 | offset 错位、大文件读不全 |
| `edit` 严格唯一匹配 | `edit` 有空白容错 | 提示与工具行为不一致 |
| 没有 `ls` | 有 `ls`（树状，过滤 .exe） | 模型不知道列目录用哪个 |
| `subagent` 可用全部工具 | `agent` 只有 `glob/grep/ls/view` | 让子代理改文件会失败 |
| 专用 `skill` 工具 | `view crush://...` 加载技能 | 模型会调用不存在的 `skill` |
| 没有 LSP 工具 | 8 个 LSP 工具 | 模型不会用 symbol 级编辑 |
| `web_search` 是主工具 | 只有 `agentic_fetch` 子代理 | 模型不知道搜索要委托 |
| `todo_write` 整表替换 | `todos` 也是整表替换 | 一致，无需改 |

### 9.1 总量对比

| | 系统提示 | 工具描述 | 合计 |
|---|---|---|---|
| DSH standard | 894 词 | 1256 词（31 个工具） | 约 2150 词 |
| Crush 现在 | 3246 词 | 2758 词（约 29 个工具） | 约 6000 词 |
| Crush 目标 | 750–900 词 | 2200–2500 词 | 约 3000–3400 词 |

Crush 的工具更多（LSP 8 个、crush_info/logs、job_*、question、agentic_fetch），
不可能压到 2150，3000 左右是合理目标。

### 9.2 规则搬家映射

| 现在在系统提示 | 目标位置 | 词数变化 |
|---|---|---|
| `editing_files` + `whitespace`（550） | `edit.md` / `multiedit.md` / `write.md`，提示里保留 80 | 550 → 80 + 工具描述 |
| `error_handling`（168） | `edit.md` 的失败段，提示保留 40 | 168 → 40 |
| `workflow` + `task_completion` + `proactiveness`（558） | 合并成 120 | 558 → 120 |
| `decision_making`（274） | 压缩成 80 | 274 → 80 |
| `critical_rules`（426） | 保留 180：安全、提交、撤销、记忆、技能、权限 | 426 → 180 |
| `communication_style` + `final_answers`（387） | 合并成 100 | 387 → 100 |
| `skills_usage`（248） | 压缩成 80 | 248 → 80 |
| `tool_usage` + `bash_commands`（227） | 80 + `bash.md` | 227 → 80 |
| `code_conventions` + `testing`（218） | 合并成 100 | 218 → 100 |
| 新增：工具路由段 | 提示里 150 | 0 → 150 |

### 9.3 Crush 专属工具要补的说明

| 工具 | 当前描述 | 要补/要改 |
|---|---|---|
| `view` | 35 词 | 0-based、默认 200 行、图片、`crush://` 技能路径 |
| `ls` | 30 词 | 树状输出、过滤隐藏文件和 .exe |
| `edit` | 65 词 | 空白兜底、读后写、何时改用 LSP |
| `multiedit` | 30 词 | 同文件多处、顺序执行 |
| `agent` | 49 词 | **只读** glob/grep/ls/view，不能改文件或执行命令 |
| `question` | 643 词 | 压到 120–150，保留单选/多选/确认页语义 |
| `lsp_*` | 共约 300 词 | 每个 1–2 句：何时优先于 grep/edit |
| `job_output` / `job_kill` | 89 词 | wait 语义、何时 kill |
| `crush_info` / `crush_logs` | 223 词 | 用于诊断 Crush 自身 |
| `agentic_fetch` | 25 词 | 何时用 `fetch`、何时委托 |
| `todos` | 27 词 | 整表替换 |
| `bash` | 1086 词 | 压到 400–500，保留后台/截断/权限 |

### 9.4 验收

- 系统提示和工具 schema 各存一份 golden 快照，防止后续漂移。
- 三类任务 A/B：单文件修改、跨文件重构、搜索类任务。
- 指标：`edit` 失败率、`multiedit`/LSP 使用率、`question` 参数错误、子代理误用。
- 如果 `edit` 失败率上升，把 exact-match 段落加回 `edit.md`，不要加回系统提示。

## 十、最终任务清单（已确认范围）

| # | 任务 | 具体范围 | 工作量 | 风险 / 备注 |
|---|---|---|---|---|
| 1 | 读工具改名 | 模型可见名 `view` → `read`；`offset` 改 1-based；默认 200 → 2000 行 | 1–1.5 天 | 建议用别名层保留内部 `view`；`MaxViewSize` 仍是 200KB，超长会截断 |
| 2 | grep 结果 | 输出改标准 `path:line:text`；上限 100 → 250；不加新参数 | 0.5–1 天 | 与 DSH 一致；DSH 也没有 `-C` |
| 3 | bash 结果 | 每条都带 `[exit code: N]`；去掉结果里的 `<cwd>`（metadata 已有） | 0.5 天 | 全局只有 2 处生产 `<cwd>`，无消费者 |
| 4 | 系统提示瘦身 | 3246 → 800–900 词；加工具路由段；编辑细节搬进 `edit.md`/`multiedit.md`/`write.md`；删重复段与两处硬冲突 | 2–3 天 | 最大风险项；用 golden + 三类任务 A/B |
| 5 | MCP 命名 | `mcp_<server>_<tool>` → `mcp__<server>__<tool>` | 0.5 天 | 需改 `mcp-tools.go`、`channelreply.go`、`docker_mcp.go`；旧会话仍能按 `mcp_` 前缀显示 |
| 6 | 上下文 reminder | AGENTS.md/技能/日期/git 从系统提示移到 hidden user reminder；cwd 保留在 runtime context | 1–2 天 | 复用现有 `HiddenUserMessage` / `preparePrompt`，不用改 UI；放在第 4 项之后单独提交 |
| 7 | 子代理 | 暂不改 | 0 | 见下 |

状态（2026-10-21，`local/dshlike`）：第 1–6 项已实现。系统提示从 3246 词降到 723 词，
上下文移到 175 词的 hidden user reminder；`go test ./...` 除 `internal/shell` 的
WSL shebang 用例外全部通过。

### 10.1 第 6 项为什么比原先估计简单

`preparePrompt` 已经有一个隐藏 user reminder（todo 提醒），`TextContent.Hidden`
和 UI 跳过 hidden 的逻辑都已存在。把 AGENTS.md、技能目录、日期、git status
拼成同样的 reminder 放到历史前面即可，不需要新表结构，也不需要 UI 改造。
Crush 没有 DSH 那种 sandbox policy，runtime context 用 cwd/日期/git/permission mode 替代。

注意：这样做的主要收益不是缓存（当前命中率已经很高），而是系统提示更短、
上下文在压缩后仍然存在、以及和 DSH 的结构一致。

### 10.2 第 7 项：子代理为什么不建议现在放开

现状：

- `agent` 实际是 task agent，工具固定为 `glob/grep/ls/view` + 只读 LSP + sourcegraph。
- task agent 的 `AllowedTools` 不包含 `agent`，所以 coder → task 只有一层。
- plan agent 可以调 `agent`，最深两层，不会无限嵌套。

放开全部工具的问题：

- 子代理是非交互的，`write`/`bash` 触发权限请求时没有稳定的审批通道。
- 子代理不跑 hooks，行为与主代理不一致。
- 每个子代理复制一份上下文，成本高。
- 一旦把 `agent` 放进 task agent 的工具表，就会真正无限嵌套。

结论：保持只读。确实需要实现型子代理时，另建一个工具，显式排除 `agent`、
限制最大深度 1，并先解决权限审批通道。

### 10.3 明确不做

web 工具形态、`question` 改造、LSP 合并、工具全局重命名、`minimal` preset。






