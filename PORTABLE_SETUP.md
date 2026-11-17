# 从零配置一台机器（PORTABLE_SETUP）

把本 fork 装到一台新机器上，从"只有一个 exe"到"mini-mcp + 真 bash + 几个 MCP"。
`<...>` 是占位符，按实际路径替换。本机的具体取值记在 `LOCAL_SETUP.md`（不进 git）。

---

## 0. 一句话

- **mini**：一个 `crush.exe` 拷贝过去就能用，零配置。
- **mini-mcp**：`crush.exe` + 若干 MCP 服务器 + 一份配置。**MCP 是可选的**，不加就是 mini。

## 1. 只要 mini：拷过去就能跑

`crush.exe` 是静态链接的单文件。`crush --mode mini` 之后：

- system prompt 一句话 + 一行 cwd；
- 工具只有 `bash`、`read_image`、`fetch`、`web_search`、`job_kill`；
- bash 走内建解释器（mvdan），**没有真 bash 也能用**，只是语义是内建子集（`&&`、管道、重定向都有）。

想让它成为默认，写全局配置 `options.mode: "mini"`（见 §3）。

## 2. 三种模式

| 模式 | system prompt | 内置工具 | MCP | 固定开销（每请求，参考值） |
|---|---|---|---|---|
| `--mode ""`（full） | coder 提示词 + 仓库上下文/技能表 | 35 个 | 挂 | ≈20k token |
| `--mode mini` | 一句 + cwd | 5 个 | 不启动 | ≈1.2k |
| `--mode mini-mcp` | 一句 + cwd + MCP instructions | 9 个（+`read`/`edit`/`write`/`job_output`） | 挂 | ≈8k（示例 MCP 裁剪后） |

mini-mcp 加回来那 4 个工具的理由：diff 预览、编辑后 LSP 诊断、带行号的读取、可回读的后台作业——shell 替代不了。
`grep`/`glob`/`ls`/`todos` 不加：shell 做得一样好。

## 3. 配置放哪，怎么合并

两个位置，后者覆盖前者：

- 全局：`~/.config/crush/crush.json`（Windows 是 `%USERPROFILE%\.config\crush\crush.json`）
- 项目：仓库根的 `crush.json`

**合并规则：对象合并，数组追加。** 这条决定了一切写法：

- 追加意味着"全局收紧 + 项目放开"天然可用（§6 的 `enabled_tools`）。
- 也意味着**不要在项目里重复写 `command`/`args`**，否则参数变成两份，CLI 报错打到 stdout，崩溃形如
  `connection closed: calling "initialize": invalid character 'c' looking for beginning of value`。

最小配置：

```jsonc
{
  "$schema": "https://charm.land/crush.json",
  "options": { "mode": "mini-mcp" }
}
```

`--mode` 命令行参数优先级高于 `options.mode`；`--mode ""` 可临时回完整模式。

## 4. 用真 bash（msys2 或 git-bash）

不配也能用（内建解释器），配了才有真正的 `sed`/`awk`/`grep`/`git`/`g++`。

```jsonc
"options": {
  "bash_path": "<bash 的绝对路径，例如 C:/msys64/usr/bin/bash.exe>",
  "shell_env": [
    "PATH=<编译器目录>;<git 的 cmd 目录>;<bash 的 usr/bin>",
    "HOME=<Windows 用户目录>"
  ]
}
```

三条硬规律：

1. **必须是绝对路径**。Windows 上 PATH 里的 `bash` 通常是 WSL，路径是 `/mnt/c/...`；msys 是 `/c/...`，两者不能混用。
2. **PATH 顺序决定工具来源**：编译器目录最前；要让 `git` 用 Windows 版就把它自己的 `cmd` 目录放在 msys 之前
   （msys 自带的 git 常常没有凭据助手，push 会卡在用户名提示），但只放 `cmd`，别放整个 `mingw64/bin`；
   把 `usr/bin` 放最后，保住 coreutils 和 `sh`（hooks 用的是 `sh`）。
3. **`HOME` 要显式指到 Windows 用户目录**，否则 msys bash 默认 `HOME=/home/<user>`，`git` 读不到
   `.gitconfig`（提交报"身份未知"），`${HOME}/...` 的 hook 路径也会指错。

附带一条：msys 的 `/tmp` 默认在安装目录下，与 `%TEMP%` 是两处；想统一就在 `/etc/fstab` 里
`<系统 temp 目录> /tmp ntfs binary,posix=0,noacl,user 0 0`，之后两个 shell 的临时文件互通。

**路径写法约定**（有个全局 skill 专门讲，叫 `repo-paths`）：仓库内文件一律**相对路径**，因为 bash 和
`read`/`edit`/`write` 的当前目录都是仓库根；临时文件放 `.crush/tmp/`（已被忽略）；确实要跨到系统 temp 时用
`cygpath -w /tmp/x` 换算，反向 `cygpath -u`。

## 5. 接一个 MCP（两个例子）

形状都是"一条 stdio 命令 + env + timeout"：

```jsonc
"mcp": {
  "<server 名>": {
    "type": "stdio",
    "command": "<启动命令>",
    "args": ["..."],
    "env": { "KEY": "VALUE" },
    "timeout": 120
  }
}
```

### 5.1 例一：PowerShell.MCP（Windows 原生能力 + 提权）

- 装法：PowerShell 7 里 `Install-Module PowerShell.MCP`。
- 用途：注册表、服务、事件日志、CIM/WMI、COM、.NET、ACL、计划任务、证书——**这些 bash 永远做不到**。
- **提权**：这个 server 本身不提升权限；用户自己开一个「以管理员身份运行」的 PowerShell 7 并
  `Import-Module PowerShell.MCP`，命令就会落到那个提权窗口里执行。

推荐配置（12 个工具里只留 5 个：工具 schema `4.55k → 2.35k`，另有 server instructions 1.1k）：

```jsonc
"pwsh": {
  "type": "stdio",
  "command": "<模块目录>/bin/win-x64/PowerShell.MCP.Proxy.exe",
  "args": ["--no-profile"],
  "timeout": 170,
  "disabled_tools": [
    // 不遥控别的机器就关掉这 6 个
    "accept_remote_console", "connect_remote_console", "disconnect_remote_console",
    "execute_remote_command", "cancel_remote_command", "wait_for_remote_completion",
    // $PWD 一行就能替代
    "get_current_location"
  ]
}
```

留下的是这 5 个，各有不可替代性：

| 工具 | 为什么必须留 |
|---|---|
| `execute_command` | 注册表 / 服务 / 事件日志 / CIM·WMI / COM / .NET / ACL / 计划任务 / 证书 / 驱动签名，以及提权 |
| `wait_for_completion` | `execute_command` 超时后命令还在跑，只有它能取回结果 |
| `cancel` | 在 pwsh 控制台里中断失控命令（bash 的 Ctrl+C 打不到） |
| `close_console` | 杀掉卡在 `Read-Host`/阻塞 IO 的控制台 |
| `start_console` | 开第二个控制台做并行/隔离；不常用，想再省 0.45k 可一起关掉 |

验证：`execute_command` 跑一句 `1+1`。


### 5.2 例二：REA（Node 写的逆向分析 server）

两种接法，二选一：

```jsonc
// 装到全局后（npm i -g rea-agents）
"rea": { "type": "stdio", "command": "rea", "args": ["mcp"], "timeout": 120 }

// 或者不装，走 npx（首次要联网，每次启动慢几百毫秒）
"rea": { "type": "stdio", "command": "npx", "args": ["-y", "rea-agents@6.9.0", "mcp"], "timeout": 120 }
```

推荐白名单（255 个工具里只留 28 个：stub `1.2k` + instructions `0.65k`；全挂时约 12k）。这 28 个是"实际用过"的并集，
够做常见的 PE/ELF 静态分析 + Frida 动态观测 + Electron 检查；不用的场景再往下删：

```jsonc
"rea": {
  "type": "stdio",
  "command": "rea",
  "args": ["mcp"],
  "timeout": 120,
  "env": { "GHIDRA_INSTALL_DIR": "<ghidra 目录>", "JAVA_HOME": "<jdk 目录>" },
  "enabled_tools": [
    // 生命周期与概览
    "open_binary", "close_binary", "binary_session", "binary_overview", "list_documents",
    // 静态分析（Ghidra/Rizin 系）
    "analyze_function", "procedure_pseudo_code", "procedure_assembly", "procedure_callers",
    "procedure_info", "read_function_instructions", "resolve_containing_procedure",
    "search_procedures", "batch_decompile", "xrefs", "read_bytes", "search_strings",
    "inspect_native_instruction", "address_to_file_offset",
    // Frida 动态观测
    "start_frida_session", "load_frida_script", "resume_frida_session", "frida_session_status",
    "unload_frida_script", "close_frida_session", "instrument_with_frida",
    // JS / Electron
    "analyze_javascript_application", "inspect_electron_fuses"
  ]
}
```

沿用同一台机器的用法统计得来的经验：

- 静态分析里 `xrefs`、`procedure_pseudo_code`、`resolve_containing_procedure`、`search_strings` 是主力，
  `list_procedures`/`list_strings` 这类"全量列举"在大型 PE 上一次几十万行，**故意不放**，用 `search_*` 代替。
- 需要新工具时先枚举真实名字再往 `enabled_tools` 里加（见 §6、§7），别凭印象写。

**推荐做法：全局 `"disabled": true`，只在要逆向的项目里打开。** 逆向是少数场景，没必要每轮都付这 1.85k。
项目侧只要 4 行（对象合并，`disabled: false` 覆盖全局；`enabled_tools` 是数组、跨文件追加，不用抄 28 个）：

```json
{
  "$schema": "https://charm.land/crush.json",
  "mcp": { "rea": { "disabled": false } }
}
```
- 数一下成本：实测每个 stub 约 171 B ≈ 43 token，所以每多 10 个工具约 +0.43k token。

- 需要 **Node ≥ 24.22.1**（6.8+ 的硬要求），装分析引擎后加 env：
  `"env": { "GHIDRA_INSTALL_DIR": "<ghidra 目录>", "JAVA_HOME": "<jdk 目录>" }`。
- 典型调用顺序：`open_binary` → `binary_overview` → `search_strings` → `xrefs` → `procedure_pseudo_code`。
- 验证：`binary_session {}`（空参数，不打开任何文件，最便宜的一次握手）。

> MCP 不止这两个：任何 stdio / http / sse 的 MCP server 都行。要点只有三条——命令能起来、env 给对、
> 工具名用 `<server 名>` 和短工具名去配置。

## 6. 省 token：MCP 的四个旋钮

工具定义在**每次请求的最前面**，装了不用的工具就是每轮都在付钱（缓存命中会打折，但仍占上下文窗口）。

| 旋钮 | 位置 | 作用 |
|---|---|---|
| `tool_search.enabled` / `min_tools` | `options.tool_search` | 工具数超过阈值才启用"只发名字 + 一句摘要，schema 按需加载" |
| `always_load` | 同上 | 哪些 server **保持完整 schema**（每轮都用才值得） |
| `enabled_tools` | `mcp.<server>` | **允许清单**：不在表里的工具根本不注册，`tool_search` 也搜不到 |
| `disabled_tools` | `mcp.<server>` | 减法，从剩下的里再摘掉 |

实测参考（同一台机器，单位 token/请求）：

- REA 255 个工具**全挂** ≈12k；`tool_search` 压过之后仍是这个量级（摘要按 200 字符封顶）。
- 改成 `enabled_tools` 只留常用的 28 个：stub ≈1.2k。
- pwsh 12 个工具**常驻**（在 `always_load` 里）≈4.55k；裁到 5 个 ≈2.35k。
- 两个 server 的 **MCP instructions** 是另一笔开销：REA 2.6KB ≈0.65k、pwsh 4.4KB ≈1.1k——工具关了说明还在。

因为数组是追加的，`enabled_tools` 可以让"全局只留常用、个别项目追加放开"：

```jsonc
// 项目 crush.json
{ "mcp": { "rea": { "enabled_tools": ["read_function_instructions"] } } }
```

反过来不行：项目**无法取消**全局的 `disabled_tools`。

详细做法（怎么枚举真实工具名、常见任务该配哪些）见 skill `rea-token-budget`。

## 7. 怎么核对工具真的挂上了

1. 直接问模型"你现在有哪些工具"——最直观。
2. `crush_info`（full / plan 模式有，mini 系列没有）看 MCP 状态与工具名。
3. 调一次最便宜的工具：pwsh `execute_command "1+1"`；REA `binary_session {}`。
4. 工具名对不上时，去 server 自己身上列：任何 MCP server 都能用一次 `initialize` + `tools/list`
   握手拿到真实表（`rea-token-budget` 的 `scripts/list-tools.mjs` 就是这么写的，可以改造成通用的）。

## 8. skills

放 `~/.config/crush/skills/<name>/SKILL.md`，让 `options.skills_paths` 指过去（默认会读这个目录）。
**只有 `description` 进提示词**（当触发条件用），正文由模型需要时 `read` 打开，所以 description 要写清"什么时候该读我"。

| skill | 干什么 |
|---|---|
| `repo-paths` | 仓库内用相对路径、临时文件放 `.crush/tmp/`、跨 shell 用 `cygpath` |
| `rea-token-budget` | REA 白名单怎么配、怎么枚举工具名、调用纪律（含 `scripts/list-tools.mjs`） |
| `github-proxy` | GitHub 连不上时的提醒（启动代理并切 TUN 模式） |

## 9. 坑

- **数组追加**：项目里重复写 `command`/`args` 会把参数翻倍，启动即崩（见 §3）。
- **npx 接法**：首次要联网下载，启动慢；离线机器用本地安装。
- **Node 版本**：REA 6.8+ 要 `^22.23.4 || ^24.22.1 || >=26.12.1`，装之前先 `node -v`。
- **yolo 模式会拦 `curl`/`wget`/`env`** 这类命令，联网请用 MCP/内置的 `fetch`、`web_search`，别在 bash 里 curl。
- **`enabled_tools` 是白名单**：server 升级后新增的工具默认不可见，要手动加，改完必须重启 Crush。
