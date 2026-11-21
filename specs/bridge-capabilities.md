# Bridge v1 能力扩展规格（bridge-capabilities）

- 状态：Draft v1，仅规格，不包含实现
- 日期：2026-11-04
- 适用范围：`E:\opencode-laptop-bridge`
- 相关文档：`README.md`、`HANDOFF.md`、`deploy/tailscale.md`、`deploy/security.md`

## 1. 背景与目标

当前 bridge 是一个 Node 24 ESM 项目：`start-bridge.mjs` 用 supergateway 4.2.1 的 config 组合模式，把 stdio MCP server 暴露成带 Bearer token 的 Streamable HTTP `/mcp`，只监听 `127.0.0.1:8765`，通过 Tailscale Serve 访问。现有成员：

- `process`：直接运行 `@wonderwhy-er/desktop-commander` 0.3.1，只放行五个进程工具。
- `screenshot`：运行 `scripts/screenshot-server.mjs`，提供 `capture_screenshot`。

现有能力缺口：

- `start_process` 没有 `workdir`，调用方只能自己拼 `cd ... && ...`；参数与 OpenCode 本地 shell 不一致。
- 没有通用文件传输通道；二进制文件不应经过 MCP 的 JSON/base64 通道。
- 没有系统信息工具，调用方无法知道默认 shell、默认目录、staging 路径、截图与剪贴板能力。
- 没有受限文件工具；DesktopCommander 的文件系统工具被整体禁用后，简单文本读写只能靠 shell。
- 没有剪贴板工具。
- `smoke-test.mjs` 当前只期望 6 个工具。

v1 目标：

1. 归一化 `start_process`：支持 `workdir`、`timeout`（兼容 `timeout_ms`）、受控 `shell` 别名、默认 workdir；保留原有五个进程工具。
2. 提供独立 HTTP 文件 staging 服务：inbox（工作站→bridge）和 outbox（bridge→工作站）；MCP 只提供 list/delete staging 工具，二进制不经过 MCP。
3. 提供 `get_system_info`，返回 OS、hostname、arch、默认 shell/路径风格、默认 workdir、workspace root、staging 路径、文件服务状态、截图能力、剪贴板能力。
4. 在 `BRIDGE_WORKSPACE` 内提供受限文本文件工具：`read_file`、`write_file`、`list_directory`、`stat_file`。
5. 提供文本剪贴板工具：`get_clipboard`、`set_clipboard`。
6. 保留现有 `capture_screenshot`，不改变语义。

项目原则不变：bridge 是通用 shell + 通用桌面能力，不理解命令用途；不添加编译、烧录、串口、adb、工具链、设备专用工具。

## 2. 非目标（v1 deferred / 已知限制）

### 2.1 PTY

v1 不提供 PTY。DesktopCommander 0.3.1 用普通管道启动子进程，没有 TTY、窗口尺寸、ANSI 终端模拟或信号控制。要支持 PTY 需要引入 `node-pty` 一类 native 依赖，或在 bridge 内替换 DesktopCommander 的进程后端；两者都会影响绿色包、Windows 构建和杀软行为，v1 不做。

影响：需要 TTY 的程序（`ssh` 密码提示、`sudo` 交互、`vim`、`top` 等）可能无法正常工作；需要交互时优先使用程序自带的非交互参数或脚本。

### 2.2 结构化 exit code

DesktopCommander 0.3.1 的快速命令只返回类似 `Process ... has finished execution` 的文本，没有稳定的数字退出码。v1 不解析、不推断、不承诺 exit code。后续如需精确退出码，应新增薄适配层或替换进程后端，并单独设计契约。

### 2.3 不镜像 OpenCode 本地能力

v1 不实现、不代理 OpenCode 的本地/编排能力，包括但不限于：`lsp`、`project_map`、`blast_radius`、`edit_batch`、`edit_transaction`、`verify_test`、`websearch`、`fetch`、`todo`、`plan`、`skill`、`task`、`dispatch`、`team`、`blackboard`。

### 2.4 不添加命令用途专用工具

不添加编译、烧录、串口、adb、设备管理、工具链探测、SDK 管理、固件、调试器等任何用途专用工具。所有命令继续通过通用 shell 执行。

### 2.5 其他 v1 不做

- 不通过 MCP 传输二进制；二进制只走 HTTP staging。
- 不在 workspace 内提供删除、移动、复制、重命名、递归遍历、二进制读写、权限修改工具；这些用 shell。
- 不提供富文本剪贴板（HTML、图片、文件列表）；只支持纯文本。
- 不修改截图语义、截图后端选择规则或截图存储布局。
- 文件服务不做 TLS、Range、断点续传、分块上传、压缩、CORS、多目录、多用户。
- 不新增 npm 依赖；实现只使用 Node 内置模块和现有 `@modelcontextprotocol/sdk`。
- 不把 bridge 变成沙箱；文件工具的路径限制只约束文件工具本身，不约束 shell。
- 不自动生成 portable 模式的 `BRIDGE_FILES_TOKEN`；文件服务保持显式启用。

## 3. 当前实现基线（实现时必须保持兼容）

- Node 24 ESM；`package.json` 中 `"type": "module"`。
- supergateway 4.2.1，Streamable HTTP `/mcp`，`--config` 组合模式，`stateful: true`，session timeout 30 分钟。
- DesktopCommander 0.3.1，`start_process` 的 `timeout_ms` 是必填字段；实际单次等待上限为 50000ms（`MAX_PROCESS_WAIT_MS`），超过后返回仍在运行的 session。
- DesktopCommander 0.3.1 的 `shell` 参数接受可执行文件名或路径；bridge 内部应传绝对可执行路径，避免用户配置中的 `defaultShell` 干扰。
- 现有工具原始名：`start_process`、`read_process_output`、`interact_with_process`、`force_terminate`、`list_sessions`、`capture_screenshot`。
- 不使用 supergateway 的 `--toolPrefix`；OpenCode 会自动加 `laptop_` 前缀。
- 当前 `scripts/start-bridge.mjs` 先计算 `BRIDGE_DATA_DIR`，后加载 `.env`；新配置模块必须先加载 `.env`，再解析全部环境变量。
- `scripts/screenshot-server.mjs` 冻结，不修改；新增能力不得改变其行为。
- `scripts/package-portable.mjs` 会递归复制整个 `scripts/`，新增脚本无需改打包脚本；但 `package.json`、`.env.example`、`opencode.example.json`、README 等仍需在实现阶段同步。

## 4. 目标架构

```text
OpenCode (workstation)
  |
  |  MCP Streamable HTTP /mcp, Bearer LAPTOP_MCP_TOKEN
  v
Tailscale Serve (tailnet only, TLS)
  |
  |  /mcp -> 127.0.0.1:8765
  |  /files -> 127.0.0.1:8766（部署方式待定，见 13.7）
  v
start-bridge.mjs (parent)
  |- file-server.mjs on 127.0.0.1:8766（仅设置 BRIDGE_FILES_TOKEN 时启动）
  |
  `- supergateway 4.2.1 on 127.0.0.1:8765
       |- process   : process-proxy.mjs  --MCP client--> DesktopCommander 0.3.1 child
       |- screenshot: screenshot-server.mjs（现有，不改）
       |- system    : system-server.mjs
       `- files     : file-mcp.mjs

Workstation 侧文件传输（独立于 MCP）：
  curl/fetch -> Tailscale Serve -> 127.0.0.1:8766/files/{inbox,outbox}/<name>
  Authorization: Bearer BRIDGE_FILES_TOKEN
```

### 4.1 成员拆分

| 成员 | 入口 | 暴露的原始工具 | 说明 |
| --- | --- | --- | --- |
| `process` | `scripts/process-proxy.mjs` | `start_process`、`read_process_output`、`interact_with_process`、`force_terminate`、`list_sessions` | 内部用 MCP client 连接 DesktopCommander；只注册五个进程工具；归一化 `start_process` |
| `screenshot` | `scripts/screenshot-server.mjs` | `capture_screenshot` | 不改 |
| `system` | `scripts/system-server.mjs` | `get_system_info`、`get_clipboard`、`set_clipboard` | 只读系统信息 + 文本剪贴板 |
| `files` | `scripts/file-mcp.mjs` | `read_file`、`write_file`、`list_directory`、`stat_file`、`list_staged_files`、`delete_staged_file` | 受限文本文件工具 + staging 管理 |

总计 15 个原始工具。

成员环境由 start-bridge 在 runtime config 中显式设置，避免依赖隐式继承：

| 成员 | 显式 env |
| --- | --- |
| `process` | `BRIDGE_DATA_DIR`、`BRIDGE_WORKSPACE`、`BRIDGE_SHELL`（如设置） |
| `screenshot` | `BRIDGE_SCREENSHOT_DIR`（现有） |
| `system` | `BRIDGE_DATA_DIR`、`BRIDGE_WORKSPACE`、`BRIDGE_FILES_ENABLED`、`BRIDGE_FILES_PORT`、`BRIDGE_SHELL`（如设置）、`BRIDGE_SCREENSHOT_BACKEND` |
| `files` | `BRIDGE_DATA_DIR`、`BRIDGE_WORKSPACE` |

任何成员的显式 env 都不得包含 `LAPTOP_MCP_TOKEN` 或 `BRIDGE_FILES_TOKEN`。

### 4.2 start-bridge 职责

1. 先加载 `<root>/.env`，再解析环境变量。
2. 校验配置，创建数据目录、workspace、staging 目录。
3. 若 `BRIDGE_FILES_TOKEN` 非空，启动 `scripts/file-server.mjs`，等待 IPC ready；失败则整体启动失败。
4. 写入 `data/supergateway.runtime.json`；该文件不得包含任何 token。
5. 启动 supergateway，打印 MCP URL、healthz URL、工具列表、workspace、staging、文件服务状态。
6. 管理 file-server 和 supergateway 的生命周期；退出时终止两者（Windows 用 `taskkill /T`，Unix 先 SIGTERM 后 SIGKILL）。
7. 不把 `LAPTOP_MCP_TOKEN`、`BRIDGE_FILES_TOKEN` 传给 supergateway 及其 MCP 成员；`SUPERGATEWAY_API_KEY` 只保留在 supergateway 进程环境，成员在启动子命令时必须清理。

### 4.3 process-proxy 内部结构

```text
supergateway --stdio--> process-proxy.mjs
                          |
                          | MCP Client + StdioClientTransport
                          v
                        node <desktop-commander dist/index.js>
```

- process-proxy 用 `createRequire(import.meta.url)` 解析 `@wonderwhy-er/desktop-commander/dist/index.js`。
- 用 `process.execPath` 启动 DesktopCommander，`cwd` 为项目根目录。
- DesktopCommander 子进程环境必须删除 `LAPTOP_MCP_TOKEN`、`SUPERGATEWAY_API_KEY`、`SUPERGATEWAY_API_KEY_FILE`、`BRIDGE_FILES_TOKEN`（修复现有“桥接 shell 可读取网关 token”的已知问题）。
- process-proxy 必须注册自己的五个工具，而不是把 DesktopCommander 的工具列表直接透传。
- DesktopCommander 断连或启动失败时，工具调用返回清晰错误 `process_backend_unavailable`；不得让未捕获异常杀死 gateway。
- stdin EOF、SIGTERM、SIGINT 时关闭 MCP client，并在宽限期后终止 DesktopCommander 子进程树。

### 4.4 文件服务生命周期

- 只有 `BRIDGE_FILES_TOKEN` 非空时才启动；未设置或空字符串视为禁用。
- 绑定固定为 `127.0.0.1`，默认端口 8766。
- 由 start-bridge 直接 spawn，不经过 supergateway。
- 子进程环境只包含文件服务需要的变量；不得包含 `LAPTOP_MCP_TOKEN`、`SUPERGATEWAY_API_KEY`。
- 通过 IPC `process.send({ type: "ready", port })` 报告监听成功；start-bridge 最多等待 10 秒。
- 监听失败（端口占用、权限）时退出非零，start-bridge 整体失败，不静默降级。
- 文件服务禁用时，`get_system_info.fileService.enabled = false`；MCP staging list/delete 工具仍可工作。

## 5. 环境变量与配置

### 5.1 变量表

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `LAPTOP_MCP_TOKEN` | 是 | 无 | MCP Bearer token；未设置则启动失败 |
| `BRIDGE_PORT` | 否 | `8765` | MCP 回环端口；监听地址固定 `127.0.0.1` |
| `BRIDGE_DATA_DIR` | 否 | `<root>/data` | 数据根目录；相对路径相对 `<root>` 解析 |
| `BRIDGE_WORKSPACE` | 否 | `<data>/workspace` | 受限文件工具根目录，也是 `start_process` 默认 workdir；相对路径相对 `<root>` 解析 |
| `BRIDGE_FILES_PORT` | 否 | `8766` | 文件服务回环端口；必须与 `BRIDGE_PORT` 不同 |
| `BRIDGE_FILES_TOKEN` | 否 | 无 | 文件服务 Bearer token；未设置或空字符串时文件服务禁用；必须与 MCP token 不同 |
| `BRIDGE_SCREENSHOT_BACKEND` | 否 | `auto` | `auto` / `ffmpeg` / `powershell`，仅 Windows 有意义；非法值启动失败 |
| `BRIDGE_MAX_FILE_BYTES` | 否 | `536870912`（512 MiB） | HTTP 单文件上传上限，正整数（字节） |
| `BRIDGE_STAGING_QUOTA_BYTES` | 否 | `2147483648`（2 GiB） | inbox + outbox 总配额，正整数（字节）；必须大于等于 `BRIDGE_MAX_FILE_BYTES` |
| `BRIDGE_SHELL` | 否 | 平台默认别名 | 只接受当前平台可用的 shell 别名；非法或不可用则启动失败 |

### 5.2 路径派生

| 路径 | 默认值 | 用途 |
| --- | --- | --- |
| `<data>` | `<root>/data` | 数据根 |
| `<workspace>` | `<data>/workspace` | 受限文件工具根、默认 workdir |
| `<data>/staging/inbox` | 固定派生 | 工作站→bridge 上传 |
| `<data>/staging/outbox` | 固定派生 | bridge→工作站下载 |
| `<data>/screenshots` | 固定派生 | 截图存储，现有行为 |
| `<data>/supergateway.runtime.json` | 固定派生 | supergateway 运行时配置，不含 token |

v1 不增加 `BRIDGE_STAGING_DIR`。staging 路径固定由 `BRIDGE_DATA_DIR` 派生。

### 5.3 配置校验

- `.env` 从 `<root>/.env` 加载；真实环境变量优先，`.env` 只补充缺失值。
- 所有 `BRIDGE_*` 相对路径相对项目根 `<root>` 解析，不相对当前工作目录。
- 端口必须是 1..65535 的整数；`BRIDGE_FILES_PORT !== BRIDGE_PORT`。
- 字节数必须是正整数；`BRIDGE_STAGING_QUOTA_BYTES >= BRIDGE_MAX_FILE_BYTES`。
- `BRIDGE_SCREENSHOT_BACKEND` 必须是 `auto|ffmpeg|powershell`（大小写不敏感，规范化为小写）。
- `BRIDGE_SHELL` 必须是当前平台允许的别名且已检测到；否则启动失败并列出可用别名。
- `BRIDGE_WORKSPACE` 创建后必须是目录；如果 workspace 等于 `BRIDGE_DATA_DIR` 或是其祖先，启动时打印警告（可能把 token、截图、staging 暴露给文件工具）。
- `BRIDGE_FILES_TOKEN` 建议至少 32 字符；v1 不强制最小长度，但拒绝空字符串。
- 如果 `BRIDGE_FILES_TOKEN` 等于 `LAPTOP_MCP_TOKEN`，启动失败；两个 token 必须不同。
- 启动失败统一写 stderr 并以非零退出，错误信息包含变量名和原因。

### 5.4 内部/派生变量

这些变量由 start-bridge 设置，不面向用户：

| 变量 | 说明 |
| --- | --- |
| `BRIDGE_FILES_ENABLED` | `"true"` / `"false"`，供 system-server 报告状态；不携带 token |
| `BRIDGE_SCREENSHOT_DIR` | 现有变量，截图目录 |

### 5.5 Token 传递规则

- `LAPTOP_MCP_TOKEN`：只用于启动 supergateway 时设置 `SUPERGATEWAY_API_KEY`；从传给 supergateway 的普通环境中删除。
- `BRIDGE_FILES_TOKEN`：只传给 file-server 子进程；从传给 supergateway 的环境中删除。
- `process-proxy` 和 `system-server` 在 spawn 外部命令时必须删除上述 token 变量。
- `screenshot-server` 在 v1 冻结；它只 spawn 固定的 ffmpeg/PowerShell 命令，但仍会继承网关环境，残余风险见 14.3。
- token 不得出现在 argv、`supergateway.runtime.json`、日志、错误文本、`get_system_info` 输出中。

## 6. 工具总览

| 原始工具 | 成员 | OpenCode 名称 | 建议权限 | 说明 |
| --- | --- | --- | --- | --- |
| `start_process` | process | `laptop_start_process` | ask | 归一化 shell 调用 |
| `read_process_output` | process | `laptop_read_process_output` | allow | 原样代理 |
| `interact_with_process` | process | `laptop_interact_with_process` | ask | 原样代理 |
| `force_terminate` | process | `laptop_force_terminate` | ask | 原样代理 |
| `list_sessions` | process | `laptop_list_sessions` | allow | 原样代理 |
| `capture_screenshot` | screenshot | `laptop_capture_screenshot` | ask | 现有，不改 |
| `get_system_info` | system | `laptop_get_system_info` | allow | 无参数，JSON 文本 |
| `get_clipboard` | system | `laptop_get_clipboard` | ask | 文本剪贴板 |
| `set_clipboard` | system | `laptop_set_clipboard` | ask | 文本剪贴板 |
| `read_file` | files | `laptop_read_file` | ask | workspace 内文本 |
| `write_file` | files | `laptop_write_file` | ask | workspace 内文本 |
| `list_directory` | files | `laptop_list_directory` | allow | 非递归 |
| `stat_file` | files | `laptop_stat_file` | allow | 元数据 |
| `list_staged_files` | files | `laptop_list_staged_files` | allow | staging 元数据 |
| `delete_staged_file` | files | `laptop_delete_staged_file` | ask | 删除 staging 文件 |

建议的 MCP annotations：

- 只读：`get_system_info`、`read_file`、`list_directory`、`stat_file`、`list_staged_files`、`get_clipboard`、`read_process_output`、`list_sessions`、`capture_screenshot`。
- 破坏性：`start_process`、`interact_with_process`、`force_terminate`、`write_file`、`delete_staged_file`、`set_clipboard`。

## 7. 进程工具契约

### 7.1 start_process

输入 schema：

| 参数 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- |
| `command` | string | 是 | 无 | 非空命令文本，原样交给 shell |
| `workdir` | string | 否 | `BRIDGE_WORKSPACE` | 必须是已存在的目录 |
| `timeout` | integer | 否 | `30000` | 单次调用等待初始输出的时间，毫秒 |
| `timeout_ms` | integer | 否 | 无 | 兼容旧参数；与 `timeout` 互斥 |
| `shell` | string | 否 | 平台默认别名 | 只接受当前平台可用别名 |
| `verbose_timing` | boolean | 否 | `false` | 透传给 DesktopCommander |

参数规则：

- `command` 必须是 string 且长度大于 0；允许前后空格，不做 trim 后执行。
- `timeout` 与 `timeout_ms` 按“字段是否存在”判断；同时出现即报错，即使值相同。
- 超时值是 1..3600000 的安全整数。DesktopCommander 0.3.1 单次等待上限为 50000ms；大于 50000 的值仍接受，但实际最多等待约 50 秒，进程继续运行，调用方应使用 `read_process_output` 继续读取。
- `timeout` 不是杀进程超时；它只决定单次调用等待多久。超时后进程会保留为 session。
- `workdir` 为空字符串、非 string、含 NUL 时报错。
- `workdir` 相对路径相对默认 workdir（`BRIDGE_WORKSPACE`）解析，允许通过 `..` 离开默认 workdir；`workdir` 不受 workspace 限制，因为 shell 本身已经拥有用户全部权限。
- `workdir` 绝对路径允许；必须存在且为目录（跟随 symlink 检查）。
- 如果默认 workdir 不存在，启动阶段应创建；process-proxy 启动时也会 `mkdir -p` 默认 workdir。用户指定的 workdir 不会被自动创建。
- `shell` 大小写不敏感，trim 后匹配别名；不接受绝对路径、可执行文件名或任意字符串。
- 内部总是向 DesktopCommander 传显式、绝对的可执行路径；不使用 DesktopCommander 用户配置里的 `defaultShell`。

shell 别名：

| 平台 | 别名 | 解析方式 | workdir 包装模板 |
| --- | --- | --- | --- |
| Windows | `powershell` | PATH 中的 `powershell.exe` | `Set-Location -LiteralPath '<p>' -ErrorAction Stop; <command>` |
| Windows | `pwsh` | PATH 中的 `pwsh.exe` | `Set-Location -LiteralPath '<p>' -ErrorAction Stop; <command>` |
| Windows | `cmd` | PATH/`%ComSpec%` 中的 `cmd.exe` | `cd /d "<p>" && <command>` |
| Windows | `git-bash` | 只解析 Git for Windows 的 `bash.exe`；不得使用 `%SystemRoot%\System32\bash.exe`（WSL） | `cd -- '<p-msys>' && <command>` |
| Unix | `bash` | PATH 或 `$SHELL` 中的 bash | `cd -- '<p>' && <command>` |
| Unix | `zsh` | PATH 或 `$SHELL` 中的 zsh | `cd -- '<p>' && <command>` |
| Unix | `sh` | PATH 中的 sh，缺失时回退 `/bin/sh` | `cd '<p>' && <command>` |
| Unix | `fish` | PATH 中的 fish | `cd -- '<p>'; and <command>` |

Windows 上裸 `bash` / `bash.exe` 明确拒绝，错误信息提示使用 `git-bash`。`git-bash` 解析顺序：`%ProgramFiles%\Git\bin\bash.exe`、`%ProgramFiles(x86)%\Git\bin\bash.exe`、`%LOCALAPPDATA%\Programs\Git\bin\bash.exe`、`git.exe` 同级的 `../bin/bash.exe`。

默认 shell：

- Windows：`powershell` → `pwsh` → `cmd`，取第一个可用的；`git-bash` 不作为默认。
- macOS/Linux：`$SHELL` 的 basename 如果在允许别名内且可用，则使用；否则 `bash`；再否则 `sh`。
- `BRIDGE_SHELL` 设置后覆盖默认值，但必须是可用别名。

路径转义：

- PowerShell：单引号包裹，内部 `'` 替换为 `''`。
- POSIX（bash/zsh/sh/git-bash）：单引号包裹，内部 `'` 替换为 `'\''`。
- `cmd`：双引号包裹；Windows 文件名不能包含 `"`，无需转义；如果 workdir 包含 `%`，返回错误 `workdir_unsupported_character`，避免 cmd 变量展开。
- `git-bash`：先把 Windows 路径转成 MSYS 形式（`C:\a b` → `C:/a b`，UNC `\\server\share` → `//server/share`），再按 POSIX 规则引用。
- 不依赖 `cd` 跨调用持久；每次 `start_process` 都重新拼接 workdir 前缀。

返回值：

- 成功时原样返回 DesktopCommander 的文本内容；保留其 `isError` 和 `structuredContent`（包括 `waitCapped` 等字段）。
- 不额外包裹 JSON，不添加 bridge 元数据。
- 校验失败返回 `isError: true`，文本格式为 `start_process failed: <code>: <message>`，消息必须包含 workdir、shell 和原因（适用时）。

错误码：

| code | 场景 |
| --- | --- |
| `invalid_arguments` | `command` 缺失/空/类型错误，或未知参数类型错误 |
| `conflicting_timeout` | `timeout` 与 `timeout_ms` 同时出现 |
| `invalid_timeout` | 非整数、超出范围 |
| `invalid_shell` | 别名不在平台白名单、未检测到、Windows 裸 bash |
| `workdir_not_found` | workdir 不存在 |
| `workdir_not_directory` | workdir 存在但不是目录 |
| `workdir_unsupported_character` | `cmd` 下 workdir 含 `%` |
| `process_backend_unavailable` | DesktopCommander 无法连接或已退出 |

示例：

```json
{
  "command": "node -e \"console.log(process.cwd())\"",
  "workdir": "subdir",
  "timeout": 10000,
  "shell": "powershell"
}
```

### 7.2 read_process_output

原样代理 DesktopCommander 0.3.1：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `pid` | number | 是 | `start_process` 返回的 session PID |
| `timeout_ms` | number | 否 | DesktopCommander 默认 5000 |
| `offset` | number | 否 | 0=从上次读取后新增；正数=绝对行；负数=末尾 tail |
| `length` | number | 否 | 最大行数 |
| `verbose_timing` | boolean | 否 | 透传 |

- 不注入默认值，不重命名参数。
- 返回内容和 `isError` 原样透传；保留 `structuredContent`。

### 7.3 interact_with_process

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `pid` | number | 是 | session PID |
| `input` | string | 是 | 发送到进程的文本 |
| `timeout_ms` | number | 否 | DesktopCommander 默认 8000 |
| `wait_for_prompt` | boolean | 否 | DesktopCommander 默认 true |
| `verbose_timing` | boolean | 否 | 透传 |

原样代理，不添加 TTY 语义。

### 7.4 force_terminate

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `pid` | number | 是 | 要终止的 session PID |

原样代理，终止 session 及其进程树。

### 7.5 list_sessions

无参数。原样代理 DesktopCommander 的会话列表。v1 不添加结构化 session 元数据。

## 8. get_system_info 契约

`get_system_info()` 无参数，返回 pretty-printed JSON 文本（2 空格缩进）。字段固定如下：

```json
{
  "os": {
    "platform": "win32",
    "type": "Windows_NT",
    "release": "10.0.26100",
    "version": "Windows 11 Pro"
  },
  "hostname": "LAPTOP",
  "arch": "x64",
  "shell": {
    "default": "powershell",
    "available": ["powershell", "pwsh", "cmd", "git-bash"],
    "pathStyle": "windows"
  },
  "paths": {
    "defaultWorkdir": "E:\\opencode-laptop-bridge\\data\\workspace",
    "workspaceRoot": "E:\\opencode-laptop-bridge\\data\\workspace",
    "dataDir": "E:\\opencode-laptop-bridge\\data",
    "staging": {
      "inbox": "E:\\opencode-laptop-bridge\\data\\staging\\inbox",
      "outbox": "E:\\opencode-laptop-bridge\\data\\staging\\outbox"
    }
  },
  "fileService": {
    "enabled": true,
    "host": "127.0.0.1",
    "port": 8766,
    "baseUrl": "http://127.0.0.1:8766/files"
  },
  "capabilities": {
    "screenshot": {
      "available": true,
      "configuredBackend": "auto",
      "detectedBackend": "ffmpeg",
      "reason": null
    },
    "clipboard": {
      "available": true,
      "backend": "powershell",
      "reason": null
    }
  }
}
```

字段规则：

- `os.platform`：`process.platform`；`os.type()`、`os.release()`、`os.version()` 对应 `type`/`release`/`version`。
- `shell.available`：当前实际检测到的别名，按平台规范顺序排列；`default` 必须在其中。
- `shell.pathStyle`：Windows 为 `"windows"`，其他为 `"posix"`。
- `paths.defaultWorkdir` 与 `paths.workspaceRoot` 在 v1 相等，但保留两个字段。
- `fileService.enabled` 来自 `BRIDGE_FILES_ENABLED`；变量缺失时回退为 `BRIDGE_FILES_TOKEN` 是否存在。禁用时 `baseUrl` 为 `null`，`host`/`port` 仍返回。
- `fileService.baseUrl` 只是回环地址，不是 Tailscale Serve 的公网 URL；bridge 不知道 serve 主机名。
- `capabilities.screenshot.available` 表示检测到可用截图后端，不保证当前有交互式桌面。
- `capabilities.clipboard.available` 只检测可执行文件，不读取剪贴板内容。
- `reason` 在不可用时给出简短原因；可用时为 `null`。
- 禁止返回：串口、USB/设备清单、adb 设备、工具链/编译器/SDK 版本、环境变量、token、剪贴板内容、进程列表、网络接口列表。

截图能力检测：

- Windows：`BRIDGE_SCREENSHOT_BACKEND=ffmpeg` 只检查 ffmpeg；`powershell` 只检查 PowerShell；`auto` 先 ffmpeg 后 PowerShell。
- macOS：检查 `screencapture`。
- Linux：要求 `DISPLAY` 或 `WAYLAND_DISPLAY`；Wayland 检查 `grim`，X11 依次检查 `scrot`、`maim`、`gnome-screenshot`、`import`。
- 该检测是 best-effort，可能与 `screenshot-server.mjs` 的实际回退顺序存在轻微差异。

剪贴板能力检测：见第 11 节。

## 9. 受限文件工具契约

### 9.1 共同路径安全规则

所有文件工具只允许访问 `BRIDGE_WORKSPACE` 内。实现必须遵循以下顺序：

1. `path` 必须是非空 string，不含 NUL，不接受 URL。
2. Windows 同时接受 `/` 和 `\` 作为分隔符；POSIX 只接受 `/`。
3. 拒绝任何 `..` 路径段（即使解析后仍在 workspace 内）。
4. Windows 拒绝 drive-relative 路径（如 `C:foo`）；绝对路径和 UNC 路径解析后必须仍在 workspace 内。
5. 相对路径相对于 workspace 根解析；绝对路径原样参与解析。
6. 启动时对 workspace 做一次 `realpath`，得到规范化根 `realRoot`；workspace 不存在时先创建。
7. 读写前对目标做 `realpath`；对不存在的写目标，对最近的已存在祖先做 `realpath`。用 `path.relative(realRoot, realTarget)` 判断包含关系：结果为空、或不是绝对路径且不以 `..` 段开头，才算在根内；否则返回 `path_escape`。
8. 读、列目录、stat 时解析 symlink；如果解析后逃出根，返回 `path_escape`。
9. 写文件时先确保父目录在根内；如果 `create_dirs` 为 true，逐级创建目录后再次校验父目录 realpath。最终写入采用同目录临时文件 + `rename`，不跟随最终组件的 symlink；如果目标已存在且是目录，返回 `not_a_file`。
10. POSIX 上打开文件时使用 `O_NOFOLLOW`（可用时）；Windows 上使用 `lstat` 拒绝 reparse point 逃逸。残余 TOCTOU 和 hard link 见安全模型。

限制：

- 路径限制只约束文件工具，不约束 `start_process`；shell 仍可访问用户有权限的一切路径。
- 不返回 URL，不访问网络。
- 所有 JSON 结果使用 2 空格缩进。

文本限制：

| 常量 | 值 | 说明 |
| --- | --- | --- |
| `MAX_TEXT_RESULT_BYTES` | 4 MiB（4194304） | `read_file` 返回内容、`write_file` 输入内容上限 |
| `MAX_TEXT_FILE_BYTES` | 32 MiB（33554432） | `read_file` 允许的源文件总大小 |
| `MAX_READ_LINES` | 5000 | `read_file` 单次 `limit` 上限 |
| `DEFAULT_READ_LINES` | 2000 | `read_file` 默认 `limit` |
| `MAX_DIRECTORY_ENTRIES` | 1000 | `list_directory` 返回条目上限 |

文本判定：

- 只支持 UTF-8 文本；读取时用 fatal 模式解码，拒绝 NUL 字节；无效 UTF-8 返回 `binary_file`。
- 读取时去掉开头的 UTF-8 BOM；写入时不自动添加 BOM。
- 行拆分前把 CRLF/CR 规范化为 LF；读取结果中的行尾统一为 `\n`。
- 空文件行数为 0；尾随换行不额外产生空行（`"a\n"` 为 1 行，`"a\n\n"` 为 2 行）。
- GBK、UTF-16 等其他编码在 v1 不支持，返回 `binary_file` 或 `io_error`，提示使用 staging 或 shell 转换。

### 9.2 read_file

输入：

| 参数 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- |
| `path` | string | 是 | 无 | workspace 内路径 |
| `offset` | integer | 否 | `1` | 起始行号，1-based |
| `limit` | integer | 否 | `2000` | 最多返回行数，1..5000 |

返回 JSON：

```json
{
  "path": "src/app.ts",
  "absolutePath": "E:\\...\\data\\workspace\\src\\app.ts",
  "offset": 1,
  "limit": 2000,
  "totalLines": 120,
  "returnedLines": 120,
  "truncated": false,
  "truncatedByBytes": false,
  "truncatedLine": false,
  "content": "..."
}
```

- `totalLines` 是规范化后的总行数。
- `offset` 超出总行数时返回空 `content`、`returnedLines: 0`、`truncated: false`。
- 选中内容超过 `MAX_TEXT_RESULT_BYTES` 时，在最后一条完整行处截断；`truncatedByBytes: true`。如果第一条选中行本身就超过上限，则返回该行前 4 MiB，`truncatedLine: true`。
- 源文件超过 `MAX_TEXT_FILE_BYTES` 返回 `file_too_large`。
- 读取目标是目录返回 `not_a_file`；不存在返回 `not_found`。

错误码：`invalid_arguments`、`invalid_path`、`path_escape`、`not_found`、`not_a_file`、`binary_file`、`file_too_large`、`permission_denied`、`io_error`。

### 9.3 write_file

输入：

| 参数 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- |
| `path` | string | 是 | 无 | workspace 内路径 |
| `content` | string | 是 | 无 | UTF-8 文本，≤ 4 MiB |
| `create_dirs` | boolean | 否 | `false` | 是否递归创建父目录 |

返回 JSON：

```json
{
  "path": "notes/today.txt",
  "absolutePath": "E:\\...\\data\\workspace\\notes\\today.txt",
  "bytes": 42,
  "created": true,
  "replaced": false
}
```

- `content` 含 NUL 返回 `invalid_text`；编码后超过 `MAX_TEXT_RESULT_BYTES` 返回 `content_too_large`。
- 父目录不存在且 `create_dirs=false` 返回 `parent_not_found`。
- 目标已存在且为目录返回 `not_a_file`。
- 目标已存在为普通文件或 symlink 时覆盖；symlink 被替换为普通文件，不跟随写入。
- 原子写入：同目录 `.<basename>.<random>.tmp` → `rename`；失败清理临时文件。
- 文件模式不保证保留；新文件遵循进程 umask（Windows 忽略 mode）。

错误码：`invalid_arguments`、`invalid_path`、`path_escape`、`invalid_text`、`content_too_large`、`parent_not_found`、`not_a_file`、`permission_denied`、`io_error`。

### 9.4 list_directory

输入：

| 参数 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- |
| `path` | string | 否 | `"."` | workspace 内目录；非递归 |

返回 JSON：

```json
{
  "path": ".",
  "absolutePath": "E:\\...\\data\\workspace",
  "entries": [
    { "name": "src", "type": "directory", "size": null, "modifiedAt": null },
    { "name": "readme.txt", "type": "file", "size": 12, "modifiedAt": "2026-11-04T00:00:00.000Z" }
  ],
  "total": 2,
  "truncated": false
}
```

- `type`：`file`、`directory`、`symlink`、`other`。
- symlink 条目只按 `lstat` 标记，不跟随；不返回 target 字符串。
- 目录项按“目录优先，然后名称大小写不敏感升序”排序。
- 超过 1000 条时截断并设置 `truncated: true`；`total` 是截断前总数。
- 路径不存在 `not_found`；不是目录 `not_a_directory`；逃逸 `path_escape`。

### 9.5 stat_file

输入：

| 参数 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- |
| `path` | string | 是 | 无 | workspace 内路径 |

返回 JSON：

```json
{
  "path": "readme.txt",
  "absolutePath": "E:\\...\\data\\workspace\\readme.txt",
  "realPath": "E:\\...\\data\\workspace\\readme.txt",
  "type": "file",
  "size": 12,
  "isSymlink": false,
  "createdAt": "2026-11-04T00:00:00.000Z",
  "modifiedAt": "2026-11-04T00:00:00.000Z"
}
```

- symlink 在 workspace 内时允许，返回目标类型、`isSymlink: true` 和解析后的 `realPath`。
- symlink 指向 workspace 外返回 `path_escape`。
- 不存在返回 `not_found`。
- `createdAt` 使用 birthtime；平台不支持时可能等于 mtime 或 epoch。

## 10. staging MCP 工具契约

### 10.1 文件名规则（HTTP 与 MCP 共用）

合法 staging 文件名：

- 解码后长度 1..128 个 Unicode code point。
- 正则：`^[\p{L}\p{N}][\p{L}\p{N}._-]*$`（Unicode 字母/数字开头；后续只允许字母、数字、`.`、`_`、`-`）。
- 不允许空格、路径分隔符、控制字符、`%`、`:`、`*`、`?`、`"`、`<`、`>`、`|`。
- 不允许以 `.` 结尾；不允许以 `.part` 结尾（大小写不敏感）。
- 不允许 Windows 保留名（大小写不敏感，去掉扩展名后判断）：`CON`、`PRN`、`AUX`、`NUL`、`CLOCK$`、`CONIN$`、`CONOUT$`、`COM1`..`COM9`、`LPT1`..`LPT9`。
- HTTP 路径段只做一次 `decodeURIComponent`；解码失败、解码后含 `/` 或 `\`、或多段路径一律拒绝。

不合法名称的文件可以存在于 staging（由 shell 创建），但不会出现在 list 结果中，也不能通过 HTTP/MCP 删除；需要用 shell 处理。

### 10.2 list_staged_files

输入：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `direction` | `"inbox"` / `"outbox"` | 是 | staging 方向 |

返回 JSON：

```json
{
  "direction": "inbox",
  "path": "E:\\...\\data\\staging\\inbox",
  "files": [
    { "name": "firmware.bin", "size": 1048576, "modifiedAt": "2026-11-04T00:00:00.000Z" }
  ],
  "totalBytes": 1048576,
  "ignoredInvalid": 0,
  "truncated": false
}
```

- 只列出普通文件；symlink、目录、`.part` 文件跳过。
- 名称必须是合法 staging 名称；不合法条目计入 `ignoredInvalid`，不出现在 `files`。
- 按名称升序排序。
- 最多返回 10000 条；超过时截断并设置 `truncated: true`。
- 不返回 token、URL 或文件内容。
- 文件服务禁用时仍可工作。

错误码：`invalid_arguments`、`invalid_direction`、`io_error`。

### 10.3 delete_staged_file

输入：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `direction` | `"inbox"` / `"outbox"` | 是 | staging 方向 |
| `name` | string | 是 | 合法 staging 名称 |

返回 JSON：

```json
{ "deleted": true, "direction": "inbox", "name": "firmware.bin" }
```

- 只允许删除普通文件或 symlink；目录返回 `not_a_file`。
- 不存在返回 `not_found`。
- 删除前校验名称和 `lstat`，不跟随 symlink 目标。
- 文件服务禁用时仍可工作。

错误码：`invalid_arguments`、`invalid_direction`、`invalid_name`、`not_found`、`not_a_file`、`permission_denied`、`io_error`。

## 11. 剪贴板契约

### 11.1 get_clipboard

无参数。返回 JSON：

```json
{ "text": "hello", "bytes": 5, "backend": "powershell" }
```

- `bytes` 是文本的 UTF-8 字节长度。
- 空剪贴板返回 `text: ""`、`bytes: 0`。
- 超过 1 MiB UTF-8 返回 `clipboard_too_large`。
- 后端不可用返回 `clipboard_unavailable`。
- 不记录、不回显剪贴板内容到 stderr 或日志。

### 11.2 set_clipboard

输入：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `text` | string | 是 | 要写入的纯文本，≤ 1 MiB UTF-8 |

返回 JSON：

```json
{ "ok": true, "bytes": 5, "backend": "powershell" }
```

- 空字符串合法，用于清空剪贴板。
- 含 NUL 返回 `invalid_text`；超过 1 MiB 返回 `clipboard_too_large`。
- 文本只能通过 stdin 传给后端，不得放进 argv、命令行、临时文件或日志。
- 不返回写入的文本内容。

### 11.3 后端

| 平台 | 后端 | 读取 | 写入 | 说明 |
| --- | --- | --- | --- | --- |
| Windows | `powershell` / `pwsh` | PowerShell `Get-Clipboard -Raw`，stdout 用 UTF-8 | PowerShell `Set-Clipboard -Value`，文本走 stdin | 优先 `powershell.exe`，缺失时 `pwsh.exe`；设置 Console Input/OutputEncoding 为 UTF-8 |
| macOS | `pbpaste` / `pbcopy` | `pbpaste` | `pbcopy` stdin | 两者都必须存在 |
| Linux Wayland | `wl-clipboard` | `wl-paste --no-newline` | `wl-copy` stdin | 要求 `WAYLAND_DISPLAY` 和两个可执行文件 |
| Linux X11 | `xclip` | `xclip -selection clipboard -o` | `xclip -selection clipboard -i` stdin | 要求 `DISPLAY` |
| Linux X11 回退 | `xsel` | `xsel --clipboard --output` | `xsel --clipboard --input` stdin | 要求 `DISPLAY` |

- 检测顺序：Windows PowerShell → macOS pbpaste/pbcopy → Wayland wl-clipboard → X11 xclip → X11 xsel。
- 检测只检查可执行文件和显示环境变量，不读取剪贴板内容。
- 后端输出按 UTF-8 严格解码；失败返回 `clipboard_error`。
- 调用外部命令时清理 token 环境变量，`windowsHide: true`。
- 建议 OpenCode 权限：`laptop_get_clipboard`、`laptop_set_clipboard` 均为 `ask`。

## 12. capture_screenshot

保持 `scripts/screenshot-server.mjs` 现有实现和语义不变：

- 无参数；返回 JSON 元数据文本 + 可选 image attachment。
- 截取 bridge 所在机器的桌面，不是工作站。
- 完整文件保存到 `<data>/screenshots`，保留 7 天，最多内联 4 MiB。
- Windows `auto` 优先 ffmpeg，回退 PowerShell；macOS `screencapture`；Linux `grim`/`scrot`/`maim`/`gnome-screenshot`/`import`。
- headless、锁屏、Session 0 可能失败。
- 建议权限 `ask`。
- v1 不增加截图参数、不改变返回格式、不迁移存储路径。

## 13. HTTP 文件服务 API

### 13.1 基础

- 绑定：`127.0.0.1`，端口 `BRIDGE_FILES_PORT`（默认 8766）。
- 协议：明文 HTTP；TLS 由 Tailscale Serve 终止。
- 路径前缀：`/files`。
- 认证：所有 `/files/*` 请求必须带 `Authorization: Bearer <BRIDGE_FILES_TOKEN>`。
- token 比较使用固定时间比较（先 SHA-256，再 `timingSafeEqual`）。
- 失败认证返回 401，`WWW-Authenticate: Bearer realm="bridge-files"`，JSON 错误体。
- 请求日志只记录 method、路径中的合法文件名、状态码、字节数、耗时；不记录 Authorization、token、查询字符串或文件内容。
- 响应头：JSON 响应 `Content-Type: application/json; charset=utf-8`、`Cache-Control: no-store`；下载响应 `X-Content-Type-Options: nosniff`。

### 13.2 PUT /files/inbox/:name

上传文件到 inbox。

请求：

- `Content-Length` 必须存在；缺失或使用 chunked 且无长度返回 411。
- `Content-Length > BRIDGE_MAX_FILE_BYTES` 返回 413，不读取 body。
- 可选 `X-Checksum-SHA256: <64 hex>`（大小写不敏感）；格式非法返回 400；流式计算不匹配返回 422，并删除临时文件。
- 实际接收字节数必须等于 `Content-Length`；不一致返回 400 `length_mismatch` 并删除临时文件。
- 内容类型忽略；按二进制流处理。

写入：

1. 校验名称。
2. 预检配额：当前 staging 总字节数 - 同名已有文件大小 + Content-Length ≤ `BRIDGE_STAGING_QUOTA_BYTES`，否则 507。并发上传使用内存中的 reserved 字节数预留配额。
3. 写入同目录临时文件 `.<name>.<random>.part`（流式，不整文件缓冲）。
4. 计算可选 SHA-256。
5. `fsync` 可选；完成后 `rename` 到目标名。目标已存在时原子替换；若平台 rename 因目标存在失败，删除目标后重试一次（仅当目标是普通文件或 symlink）。如果目标是目录，返回 400 `not_a_file`。
6. 成功响应：
   - 新文件：201
   - 覆盖：200
   - body：`{"direction":"inbox","name":"<name>","size":123,"sha256":"<hex>","replaced":false}`
7. 任何失败、客户端断开、校验失败：删除 `.part`，释放预留配额。

### 13.3 GET /files/outbox/:name

下载 outbox 文件。

- 只允许普通文件；symlink、目录、`.part` 统一返回 404，避免探测。
- 用 `lstat` 和 realpath 确保父目录是 outbox 根。
- 200，流式响应，带：
  - `Content-Type: application/octet-stream`
  - `Content-Length`
  - `Content-Disposition: attachment; filename="<ascii-fallback>"; filename*=UTF-8''<percent-encoded>`
- 不支持 Range、HEAD、压缩。HEAD 返回 405。

### 13.4 GET /files/outbox

列出 outbox。

- 200，JSON：

```json
{
  "direction": "outbox",
  "files": [
    { "name": "result.bin", "size": 123, "modifiedAt": "2026-11-04T00:00:00.000Z" }
  ],
  "totalBytes": 123,
  "truncated": false
}
```

- 只列合法名称的普通文件；symlink、目录、`.part` 跳过。
- 名称升序；最多 10000 条。
- 不返回下载 URL、token 或内容。

### 13.5 DELETE /files/inbox/:name 与 DELETE /files/outbox/:name

- 校验名称。
- 只删除普通文件或 symlink；目录返回 400 `not_a_file`。
- 成功 204，无 body。
- 不存在 404。
- 不跟随 symlink。

### 13.6 配额、TTL、清理

- 配额统计 inbox + outbox 的普通文件大小；symlink 不计入。
- `BRIDGE_STAGING_QUOTA_BYTES` 是硬上限；并发上传通过 reserved 字节预检。
- 启动时和之后每小时清理一次：
  - inbox/outbox 中 mtime 超过 7 天的普通文件删除。
  - `.part` 临时文件超过 1 小时删除。
  - 清理是 best-effort，日志只记录数量和字节数。
- TTL 在 v1 固定为 7 天，不提供环境变量。
- 不执行、不解析、不自动导入上传文件；不自动解压。

### 13.7 Tailscale Serve 暴露（部署阶段必须实测）

文件服务是独立端口，当前 `tailscale serve --bg 8765` 不会暴露 8766。v1 不改变 bridge 只绑定回环地址的原则，工作站通过 Tailscale Serve 访问。两种候选方案：

方案 A（推荐，独立 HTTPS 端口，避免与 `/mcp` 路由互相影响）：

```bash
tailscale serve --bg --https=8443 http://127.0.0.1:8766
```

选择 8443 时，Tailscale ACL 必须同时允许工作站访问笔记本的 `tcp:8443`，不能只放行 `tcp:443`。

工作站 URL：

```text
https://<laptop>.<tailnet>.ts.net:8443/files/inbox/<name>
https://<laptop>.<tailnet>.ts.net:8443/files/outbox/<name>
```

方案 B（同一 443 端口按路径分流）：

```bash
tailscale serve reset
tailscale serve --bg --set-path /mcp http://127.0.0.1:8765
tailscale serve --bg --set-path /files http://127.0.0.1:8766
```

方案 B 必须先验证 Tailscale Serve 是否保留完整 `/files` 路径，以及是否影响 `/mcp` 和 healthz。未验证前不要把方案 B 写入用户文档作为默认。

`get_system_info.fileService.baseUrl` 始终返回回环地址；工作站使用的公网 URL 由部署配置决定，bridge 不猜测。

### 13.8 工作站使用示例

```bash
# 上传
curl --fail-with-body -X PUT \
  -H "Authorization: Bearer $BRIDGE_FILES_TOKEN" \
  -H "X-Checksum-SHA256: $(sha256sum payload.bin | cut -d' ' -f1)" \
  --data-binary @payload.bin \
  "https://<laptop>.<tailnet>.ts.net:8443/files/inbox/payload.bin"

# 下载
curl --fail-with-body -OJ \
  -H "Authorization: Bearer $BRIDGE_FILES_TOKEN" \
  "https://<laptop>.<tailnet>.ts.net:8443/files/outbox/result.bin"
```

## 14. 安全模型

### 14.1 信任边界

- bridge 仍是通用 shell，不是沙箱。持有 MCP token 的客户端等价于拥有 bridge 用户的交互登录权限。
- 文件工具的 workspace 限制不是针对 MCP 调用者的安全边界（调用者可以直接用 `start_process` 读任意文件）；它用于降低模型误操作和路径错误的风险。
- 文件服务 token 与 MCP token 分离，允许把“只能传文件”的能力与“能执行命令”的能力分开授权。两者都在 Tailscale ACL 之内。

### 14.2 必须保持

- 只监听 `127.0.0.1`；禁止 `0.0.0.0`、路由器端口转发、公网反向代理、Tailscale Funnel。
- MCP token 必填；文件服务启用时文件 token 必填。
- token 不进入仓库、argv、日志、`supergateway.runtime.json`、`get_system_info`、错误文本。
- `BRIDGE_FILES_TOKEN` 不传给 supergateway 及其成员。
- `process-proxy` 和 `system-server` spawn 外部命令时清理 token 环境变量。
- `LAPTOP_MCP_TOKEN` 不传给 bridged shell。
- 文件服务只暴露 inbox/outbox；不使用用户提供的路径；名称解析后必须仍在对应 staging 根内。
- 上传文件不执行、不解析、不自动解压。
- 以普通用户运行，不使用 root/Administrator，不使用 Windows Session 0 服务。
- 剪贴板和截图权限建议 `ask`。

### 14.3 文件服务路径防御

- 只接受单个路径段；解码后拒绝 `/`、`\`、`..`、NUL。
- 名称白名单和 Windows 保留名检查。
- `lstat` 拒绝 symlink 下载；上传/删除用固定 staging 根 + 校验名称，不拼接用户目录。
- 上传使用 `.part` + rename；失败清理。
- 只读 loopback，TLS 由 Tailscale Serve 终止。

残余风险：

- staging 目录可被 bridge shell 修改；恶意 shell 可以创建 symlink/硬链接。文件服务拒绝跟随 symlink，但无法防御 hard link 和 realpath/open 之间的 TOCTOU。该风险与“shell 已可执行任意命令”属于同一信任边界。
- workspace 文件工具同样存在 symlink 竞态窗口；v1 用 realpath + `O_NOFOLLOW` + 临时文件 rename 降低风险，但不承诺对抗同用户恶意进程。
- 如果用户把 `BRIDGE_WORKSPACE` 设为 `BRIDGE_DATA_DIR` 或项目根，文件工具可能读到 token、截图或 staging；启动时只警告，不强制阻止。
- `screenshot-server.mjs` 在 v1 不修改，它的 ffmpeg/PowerShell 子进程会继承 `SUPERGATEWAY_API_KEY`；这些子命令是固定的，不接受用户传入的命令文本，但同用户进程仍可能读取该环境变量。后续如需彻底清理，应在修改截图 server 时单独评审。

### 14.4 建议的 OpenCode 权限

```json
{
  "permission": {
    "laptop_start_process": "ask",
    "laptop_read_process_output": "allow",
    "laptop_interact_with_process": "ask",
    "laptop_force_terminate": "ask",
    "laptop_list_sessions": "allow",
    "laptop_capture_screenshot": "ask",
    "laptop_get_system_info": "allow",
    "laptop_get_clipboard": "ask",
    "laptop_set_clipboard": "ask",
    "laptop_read_file": "ask",
    "laptop_write_file": "ask",
    "laptop_list_directory": "allow",
    "laptop_stat_file": "allow",
    "laptop_list_staged_files": "allow",
    "laptop_delete_staged_file": "ask"
  }
}
```

## 15. 错误语义

### 15.1 MCP 工具错误

统一返回：

```json
{
  "isError": true,
  "content": [{ "type": "text", "text": "<tool> failed: <code>: <message>" }]
}
```

- 不抛出未捕获异常；工具 handler 内部捕获并转换。
- 错误消息包含足够上下文（路径、workdir、shell、方向、名称），但不包含 token、环境变量或文件内容。
- DesktopCommander 的透传工具保留其原始错误文本和 `isError`，不强制改写。
- `start_process` 的校验错误使用第 7.1 节错误码；文件工具使用第 9 节错误码；staging 使用第 10 节；剪贴板使用第 11 节。

### 15.2 HTTP 错误

统一 JSON：

```json
{ "error": { "code": "invalid_name", "message": "..." } }
```

| 状态 | code | 场景 |
| --- | --- | --- |
| 400 | `bad_request` | URL/路径格式错误 |
| 400 | `invalid_name` | 文件名不合法 |
| 400 | `invalid_checksum_header` | checksum 头格式非法 |
| 400 | `length_mismatch` | 实际字节数与 Content-Length 不一致 |
| 400 | `not_a_file` | 目标是目录 |
| 401 | `unauthorized` | 缺失/错误 Bearer token |
| 404 | `not_found` | 文件或路由不存在 |
| 405 | `method_not_allowed` | 不支持的方法 |
| 411 | `length_required` | PUT 缺少 Content-Length |
| 413 | `file_too_large` | 单文件超限 |
| 422 | `checksum_mismatch` | SHA-256 不匹配 |
| 500 | `io_error` | 文件系统错误 |
| 507 | `quota_exceeded` | staging 配额不足 |

### 15.3 启动错误

- 缺少 `LAPTOP_MCP_TOKEN`：退出 1，提示设置环境变量或 `.env`。
- 配置非法（端口、字节数、backend、shell）：退出 1，信息包含变量名和非法值。
- 文件服务启用但监听失败：退出 1，同时终止已启动的子进程。
- 依赖解析失败（supergateway、DesktopCommander、MCP SDK）：退出 1，提示 `bun install`。
- 任何启动错误不得打印 token。

## 16. 测试计划

### 16.1 smoke-test.mjs 必须覆盖

现有测试保留并改造：

- 健康检查 `/healthz`。
- 未认证和错误 token 的 MCP 请求返回 401。
- MCP SDK 连接、工具列表。
- 工具列表恰好为以下 15 个（排序后比较）：

```text
capture_screenshot
delete_staged_file
force_terminate
get_clipboard
get_system_info
interact_with_process
list_directory
list_sessions
list_staged_files
read_file
read_process_output
set_clipboard
start_process
stat_file
write_file
```

- `start_process`：
  - `echo bridge-ok` 使用 `timeout`（新参数）成功。
  - 旧参数 `timeout_ms` 仍成功。
  - 同时传 `timeout` 和 `timeout_ms` 返回错误。
  - `workdir` 为新建的 workspace 子目录时，`node -e "console.log(process.cwd())"` 输出解析后的目录。
  - 相对 `workdir` 相对默认 workdir 解析。
  - 不存在的 workdir 返回错误，消息包含 workdir。
  - 非法 shell（Windows 传 `bash`；Unix 传 `cmd`）返回错误；显式合法 shell 成功。
  - 长任务启动、`read_process_output`、`list_sessions`、`force_terminate`。
  - `interact_with_process` 交互。
- `get_system_info`：
  - 返回可解析 JSON。
  - 包含 `os`、`hostname`、`arch`、`shell`、`paths`、`fileService`、`capabilities`。
  - 不包含 `serial`、`devices`、`toolchain`、`env` 等禁止字段。
  - 文件服务启用时 `fileService.enabled=true` 且端口匹配。
- 受限文件工具：
  - `write_file` + `read_file` UTF-8 中文往返。
  - `offset`/`limit` 行读取。
  - `create_dirs`。
  - `list_directory`、`stat_file`。
  - `../` 穿越、绝对路径逃逸、symlink 逃逸（Windows 无权限创建 symlink 时跳过）返回 `path_escape`。
  - 写入 NUL 或读取二进制文件返回错误。
- staging MCP：
  - `list_staged_files` 两种 direction。
  - `delete_staged_file` 成功和不存在错误。
- 剪贴板：
  - `get_system_info.capabilities.clipboard.available` 为 true 时，`set_clipboard` + `get_clipboard` 往返。
  - 不可用时验证返回清晰错误或按测试环境跳过。
- 截图：
  - 保持现有行为：有 image 时校验元数据；无桌面时允许失败，不视为 smoke 失败。
- HTTP 文件服务：
  - smoke 启动 bridge 时设置 `BRIDGE_FILES_TOKEN` 和一个空闲的 `BRIDGE_FILES_PORT`。
  - 未认证、错误 token 的 PUT/GET/DELETE 返回 401。
  - 用 MCP token 访问文件服务返回 401（token 分离）。
  - PUT inbox、可选 checksum、GET outbox（测试进程直接或通过 shell 把文件从 inbox 移到 outbox）、GET 列表、DELETE 两种方向。
  - 文件名 `../evil`、`..%2Fevil`、`CON`、`a/b` 返回 400。
  - checksum 不匹配返回 422 且不留下文件。
  - 使用较小的 `BRIDGE_MAX_FILE_BYTES`（如 64 KiB）测试 413；使用较小 quota 测试 507。
  - 下载响应包含 `Content-Disposition`。
- token 隔离：
  - `start_process` 打印环境中 `LAPTOP_MCP_TOKEN`、`BRIDGE_FILES_TOKEN`、`SUPERGATEWAY_API_KEY` 均为空（验证 4.3 的清理）。
  - `supergateway.runtime.json` 不包含两个 token 的字符串。
- 文件服务禁用场景（可独立测试或第二个 bridge 实例）：
  - 未设置 `BRIDGE_FILES_TOKEN` 时 `get_system_info.fileService.enabled=false`，`baseUrl=null`，端口无监听。

### 16.2 聚焦测试（不放进 smoke 的慢/难场景）

- TTL 清理：用 `utimes` 构造 7 天前的文件，调用清理函数；构造 1 小时前的 `.part`。
- 并发上传配额预留。
- Content-Length 与实际不一致、客户端中断上传后的 `.part` 清理。
- Windows rename 覆盖已有文件的路径。
- 文件名 Unicode、超长名称、保留名。
- 剪贴板后端编码：中文、多行、空字符串、末尾换行。
- workspace 路径包含空格、中文、`..` 段、不同盘符、UNC。
- 大文件流式上传/下载的哈希一致性。

### 16.3 跨平台与跳过策略

- Windows：完整 smoke；截图可能因无交互桌面跳过；git-bash 可用时测试 `git-bash`。
- Linux headless：截图和剪贴板按可用性跳过，必须打印跳过原因；路径/文件/staging/进程测试必须通过。
- macOS：截图/剪贴板后端按可用性测试。
- 任何跳过都必须显式输出原因，不能用静默跳过掩盖失败。

### 16.4 手工/部署验证

- 通过 Tailscale Serve 从工作站执行 HTTP PUT/GET/列表/删除。
- 工作站用错误 token 和 MCP token 访问文件服务，均应 401。
- 工作站 MCP 调用 `get_system_info` 的 hostname 与笔记本一致。
- 交互式桌面下验证截图和剪贴板。
- 便携包内运行 smoke（`runtime/node.exe app/scripts/smoke-test.mjs`）。
- 服务重启后 token 轮换生效。

## 17. 验收标准

1. 工具列表恰好为 15 个，名称与第 16.1 节一致；不泄露 DesktopCommander 其他工具。
2. `node scripts/smoke-test.mjs` 在 Windows 开发机通过；Linux headless 在文档化跳过后通过。
3. `start_process` 支持 `workdir`、`timeout`/`timeout_ms`、受控 shell 别名、默认 workdir；冲突、非法路径、非法 shell 均返回清晰错误。
4. 文件工具只能访问 workspace； `..`、绝对路径逃逸、symlink 逃逸测试通过；UTF-8 文本读写往返正确。
5. 文件服务启用后，Bearer 认证、流式上传、下载、列表、删除、checksum、单文件上限、总配额、TTL 清理、`.part` 原子性均按第 13 节工作。
6. `BRIDGE_FILES_TOKEN` 未设置时文件服务不监听；`get_system_info` 正确报告禁用状态。
7. 文件 token 与 MCP token 分离；错误 token 互不接受；两个 token 不出现在 argv、运行时配置、日志或 `get_system_info`。
8. bridged shell 无法读取 `LAPTOP_MCP_TOKEN`、`BRIDGE_FILES_TOKEN`、`SUPERGATEWAY_API_KEY`。
9. `get_system_info` 返回第 8 节字段，且不包含串口、设备、工具链、环境变量等禁止信息。
10. 剪贴板可用时文本往返正确；不可用时返回清晰错误；权限示例为 `ask`。
11. `capture_screenshot` 行为、返回格式和存储位置与现有实现一致。
12. 不新增 npm 依赖；不添加任何命令用途专用工具；不修改 `screenshot-server.mjs` 语义。
13. README、HANDOFF、`.env.example`、`opencode.example.json`、`deploy/tailscale.md`、`deploy/security.md` 在实现完成后同步更新。
14. 便携包重新生成后，包含所有新脚本且能运行 smoke；Tailscale Serve 文件路由已实测。

## 18. 分阶段实施建议

### Phase 0：契约与共享配置（阻塞项，约 0.5 天）

- 冻结本 spec。
- 新增 `scripts/lib/bridge-config.mjs`：加载 `.env`、解析/校验环境变量、派生路径、校验 backend/shell、提供脱敏日志对象。
- 新增 `scripts/lib/shells.mjs` 的检测/别名/包装接口，Phase 0 冻结，供 process 和 system 共同使用。
- `start-bridge.mjs` 改用配置模块，先加载 `.env` 再解析；保持现有 6 个工具行为不变。
- 定义文件/目录创建、token 清理、子进程关闭的工具函数。
- 增加配置单元测试（非法端口、quota < max、非法 shell、workspace 警告）。

退出标准：现有 smoke 仍通过；配置模块接口冻结，供其他工作流并行使用。

### Phase 1：process-proxy + start_process 归一化（可与 Phase 2/3 并行）

- 使用 Phase 0 的 `scripts/lib/shells.mjs`；如实现中发现缺口，回到 Phase 0 接口评审，不在 process-proxy 内复制检测逻辑。
- 新增 `scripts/process-proxy.mjs`：MCP client 连接 DesktopCommander，注册五个工具，清理子进程 token，处理退出。
- `start-bridge.mjs` 的 `process` 成员改为 `process-proxy.mjs`。
- 更新 smoke 的进程部分。

退出标准：五个进程工具全部通过；`workdir`/`timeout`/`shell` 测试通过；DesktopCommander 崩溃不拖垮 gateway。

### Phase 2：受限文件工具（可与 Phase 1/3 并行）

- 新增 `scripts/lib/safe-paths.mjs`、`scripts/file-mcp.mjs`。
- 实现 workspace 路径校验、文本检测、read/write/list/stat。
- 更新 smoke 的文件工具部分。

退出标准：第 9 节契约和路径逃逸测试通过。

### Phase 3：HTTP 文件服务 + staging 工具（需要 Phase 0；与 Phase 1/2 并行）

- 新增 `scripts/lib/staging.mjs`、`scripts/file-server.mjs`。
- file-mcp 增加 `list_staged_files`、`delete_staged_file`。
- start-bridge 集成 file-server 生命周期、IPC ready、端口/shutdown、token 清理。
- 更新 smoke 的 HTTP/staging 部分。

退出标准：第 13 节 API、配额、checksum、原子性测试通过；文件服务禁用路径正确。

### Phase 4：system-server（可与 Phase 1/2/3 并行）

- 新增 `scripts/system-server.mjs`、`scripts/lib/clipboard.mjs`。
- 实现 `get_system_info`、`get_clipboard`、`set_clipboard` 和三平台后端。
- 更新 smoke 的系统信息/剪贴板部分。

退出标准：第 8、11 节契约通过；headless 平台优雅降级。

### Phase 5：集成、文档、部署验证

- 合并所有成员，更新 `start-bridge.mjs` 完整配置和工具列表。
- 更新 smoke 工具总数和全部场景。
- 更新 README、HANDOFF、`.env.example`、`opencode.example.json`、deploy 文档。
- 重新生成便携包并运行包内 smoke。
- 实测 Tailscale Serve 文件路由（方案 A 或 B），确认 ACL 仍只允许工作站。
- 完成第 16.4 节手工验证。

退出标准：第 17 节全部满足。

### 并行依赖图

```text
Phase 0 (配置冻结)
  |-- Phase 1 process --------|
  |-- Phase 2 file tools -----|--- Phase 5 集成/文档/部署
  |-- Phase 3 file server ----|
  `-- Phase 4 system ---------|
```

Phase 1/2/3/4 之间不共享可写文件；共享模块（`bridge-config.mjs`、`shells.mjs`）由 Phase 0 冻结。Phase 4 依赖 Phase 0 的 shell 检测接口，但不依赖 Phase 1 的 process-proxy 实现。

## 19. 文件所有权建议

| 文件 | 所有者 | 说明 |
| --- | --- | --- |
| `specs/bridge-capabilities.md` | 架构/规格 | 本文件；实现阶段只读 |
| `scripts/lib/bridge-config.mjs` | 集成 | 环境解析、路径派生、校验、脱敏；Phase 0 冻结 |
| `scripts/start-bridge.mjs` | 集成 | 成员配置、file-server 生命周期、token 清理、关闭 |
| `scripts/lib/shells.mjs` | 进程工作流（Phase 0 交付并冻结） | shell 检测、别名、cd 包装；system-server 只读复用检测结果 |
| `scripts/process-proxy.mjs` | 进程工作流 | DesktopCommander MCP client + 五个工具 |
| `scripts/lib/safe-paths.mjs` | 文件工作流 | workspace 路径安全、文本读写 |
| `scripts/lib/staging.mjs` | 文件工作流 | staging 名称、list/delete、配额、TTL |
| `scripts/file-mcp.mjs` | 文件工作流 | 四个受限文件工具 + 两个 staging 工具 |
| `scripts/file-server.mjs` | 文件工作流 | HTTP 文件服务 |
| `scripts/lib/clipboard.mjs` | 系统工作流 | 三平台剪贴板后端 |
| `scripts/system-server.mjs` | 系统工作流 | `get_system_info` + 剪贴板工具 |
| `scripts/smoke-test.mjs` | 测试/集成 | 只有测试所有者修改；其他工作流提交测试用例需求 |
| `scripts/screenshot-server.mjs` | 冻结 | 任何人不得修改行为 |
| `package.json` / `bun.lock` | 集成 | 默认不改；新增依赖必须单独批准 |
| `README.md` / `HANDOFF.md` | 文档 | Phase 5 统一更新 |
| `.env.example` / `opencode.example.json` | 集成/文档 | Phase 5 更新；不放真实 token |
| `deploy/*` | 文档/部署 | Phase 5 更新 Tailscale 与安全说明 |

协作规则：

- 共享模块（`bridge-config.mjs`）接口在 Phase 0 后冻结；消费方不得复制一份解析逻辑。
- 每个工作流只修改自己拥有的文件；跨文件需求通过 lead 协调。
- `smoke-test.mjs` 只由测试所有者修改，避免并行冲突；其他工作流用独立临时脚本验证自己的部分。
- `start-bridge.mjs` 只由集成所有者修改；进程/系统/文件工作流通过环境变量和成员入口契约接入。
- 不修改 `screenshot-server.mjs`；截图能力只在 `system-server.mjs` 中做只读检测。
- 文档在行为稳定后统一更新，避免多份互相矛盾的说明。

## 20. 已定决策与待确认事项

已定：

- 保持 supergateway config 组合模式，新增 `process-proxy`、`system`、`files` 三个成员。
- `start_process` 的默认 workdir 是 `BRIDGE_WORKSPACE`；这是相对当前“DesktopCommander 在项目根启动”的行为变化。
- `timeout` 是单次等待输出时间，不是杀进程超时；DesktopCommander 0.3.1 实际单次等待上限约 50 秒。
- 文件工具只支持 UTF-8 文本；二进制走 staging。
- staging 名称白名单不包含空格；需要空格的文件名由工作站重命名。
- 文件服务与 MCP token 分离；文件服务默认禁用，显式设置 token 才启用。
- staging TTL 固定 7 天，v1 不提供环境变量。

已定（lead 决策，2026-11-04）：

1. Tailscale 文件路由采用方案 A：独立 HTTPS 端口 8443，映射到 127.0.0.1:8766；不默认改动 443 上的 /mcp 路由。部署文档必须同时提示 ACL 放行 tcp:8443。方案 B 仅作为后续可选验证项。
2. 默认 workdir 改为 BRIDGE_WORKSPACE；这是有意的行为变化，README/HANDOFF 必须写明迁移说明。需要项目根相对路径的调用方应显式传 workdir。
3. staging 文件名白名单不接受空格；工作站需要先重命名。
4. Linux 剪贴板依赖 wl-clipboard/xclip/xsel 之一；不可用时工具返回 clipboard_unavailable，安装说明写入 README，不新增 npm 依赖。
5. 便携包 v1 不自动生成 BRIDGE_FILES_TOKEN；文件服务保持显式启用，避免无意暴露文件端口。
6. 允许把 zod 声明为直接依赖（当前已通过 SDK/DesktopCommander 安装），实现 MCP 工具 schema 时使用；不引入其他新库。
