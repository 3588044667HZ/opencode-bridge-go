# Bridge 行为冻结 v1（bridge-behavior-freeze-v1）

- 状态：Frozen（规范性文档）
- 契约版本：`bridge-v1`
- 冻结日期：2026-11-04
- 适用对象：`E:\opencode-laptop-bridge` 当前 JavaScript v1 实现
- 机器可检查的工具面：`specs/bridge-tools-v1.snapshot.json`

## 1. 目的与冻结边界

本文冻结 bridge 在 **MCP/HTTP 黑盒边界**上可观察的行为，用于在保留 DesktopCommander 后端的前提下，约束后续重构、替换实现或更换编程语言时不得破坏现有客户端。

冻结范围：

- MCP `tools/list` 的工具名、title、annotations 和输入 schema。
- MCP 工具的输入语义、输出语义、错误码和进程会话语义。
- 系统信息、workspace 文件工具、staging MCP、HTTP 文件服务、剪贴板、截图的可观察行为。
- 两个 Bearer token 的隔离与文件服务启用/禁用行为。

不在冻结范围：

- Node.js 内部模块划分、函数名、文件布局和实现语言。
- 日志格式、日志文本、子进程名称、PID、端口分配过程和启动耗时。
- MCP 工具的 `description`、instructions、提示语、emoji 和其他宣传性文字；这些内容可以改写，但不得与本文规定的行为矛盾。
- 测试脚本的内部实现；测试脚本只需保持本文第 17 节的黑盒覆盖能力。

规范性优先级：

1. 本文件规定行为语义和兼容性策略。
2. `specs/bridge-tools-v1.snapshot.json` 规定工具名、title、annotations 和输入 schema 的精确机器形状。
3. `specs/bridge-capabilities.md` 与 `specs/bridge-object-api.md` 提供更细的语义和对象生命周期说明；与本文件冲突时以本文件为准，并应修正文档。

## 2. 版本与基线

- 契约版本固定为字符串 `"bridge-v1"`。
- 当前实现基线：Node.js v1 多成员 MCP 栈，进程成员保留 DesktopCommander 0.3.1 作为后端。
- v1 不要求替换 DesktopCommander；但任何替换必须满足第 17 节的一致性测试。
- 破坏性变化必须升级为 `bridge-v2`，并同步更新客户端和本冻结文档。

## 3. 冻结的工具面

MCP 必须暴露且仅暴露以下 15 个原始工具。OpenCode 会自动添加 `laptop_` 前缀；bridge 自身不得再添加工具前缀。

| 原始工具名 | OpenCode 名称 | 成员 |
| --- | --- | --- |
| `start_process` | `laptop_start_process` | process |
| `read_process_output` | `laptop_read_process_output` | process |
| `interact_with_process` | `laptop_interact_with_process` | process |
| `force_terminate` | `laptop_force_terminate` | process |
| `list_sessions` | `laptop_list_sessions` | process |
| `capture_screenshot` | `laptop_capture_screenshot` | screenshot |
| `get_system_info` | `laptop_get_system_info` | system |
| `get_clipboard` | `laptop_get_clipboard` | system |
| `set_clipboard` | `laptop_set_clipboard` | system |
| `read_file` | `laptop_read_file` | files |
| `write_file` | `laptop_write_file` | files |
| `list_directory` | `laptop_list_directory` | files |
| `stat_file` | `laptop_stat_file` | files |
| `list_staged_files` | `laptop_list_staged_files` | files |
| `delete_staged_file` | `laptop_delete_staged_file` | files |

约束：

- 不得把 DesktopCommander 的其他工具直接透传或注册为额外 MCP 工具。
- 不得重命名、合并或删除上述工具。
- 工具顺序不稳定；一致性检查按名称排序后比较。
- 新增工具属于 v1 工具面之外的扩展，默认需要 `bridge-v2`；仅当新增能力完全通过现有工具的可选参数和加法字段表达时才不需要升版。

## 4. 冻结的输入 schema

精确的 JSON Schema（含键、类型、required、注解字段及 `$schema` 键是否存在）以 `specs/bridge-tools-v1.snapshot.json` 为准。语义摘要如下。

title 与 annotations 同样由 snapshot 精确检查；工具层和 schema 内的 `description` 文本都不进入 snapshot，属于第 16 节允许变化的说明文字。

### 4.1 进程工具

| 工具 | 参数（required 加粗） |
| --- | --- |
| `start_process` | **`command`: string**；`workdir`: string；`timeout`: integer；`timeout_ms`: integer；`shell`: string；`verbose_timing`: boolean |
| `read_process_output` | **`pid`: number**；`timeout_ms`: number；`offset`: number；`length`: number；`verbose_timing`: boolean |
| `interact_with_process` | **`pid`: number**；**`input`: string**；`timeout_ms`: number；`wait_for_prompt`: boolean；`verbose_timing`: boolean |
| `force_terminate` | **`pid`: number** |
| `list_sessions` | 无参数 |

### 4.2 系统与截图工具

| 工具 | 参数 |
| --- | --- |
| `get_system_info` | 无参数 |
| `get_clipboard` | 无参数 |
| `set_clipboard` | **`text`: string** |
| `capture_screenshot` | 无参数 |

### 4.3 workspace 与 staging 工具

| 工具 | 参数 |
| --- | --- |
| `read_file` | `path`: string；`offset`: number；`limit`: number |
| `write_file` | `path`: string；`content`: string；`create_dirs`: boolean |
| `list_directory` | `path`: string |
| `stat_file` | `path`: string |
| `list_staged_files` | `direction`: string |
| `delete_staged_file` | `direction`: string；`name`: string |

注意：文件工具和 staging 工具的部分参数字段在 v1 schema 中为 optional，这是既有的机器形状；语义上 `read_file.path`、`write_file.path`、`write_file.content`、`stat_file.path`、`list_staged_files.direction`、`delete_staged_file.direction/name` 仍是必填的，缺失时返回 `invalid_arguments`。替换实现不得通过把字段改为 required 或改变字段类型来破坏现有客户端。

### 4.4 annotations

每个工具的 `readOnlyHint`、`destructiveHint`、`openWorldHint` 以 snapshot 为准。客户端可能用这些提示决定是否请求用户确认，因此不得反向改变其语义。

## 5. 错误模型

### 5.1 MCP 工具错误

被 bridge 校验或转换的错误统一返回：

```json
{
  "isError": true,
  "content": [{ "type": "text", "text": "<tool> failed: <code>: <message>" }]
}
```

- 工具 handler 不得把未捕获异常直接抛给 MCP 客户端。
- 错误消息可以包含路径、workdir、shell、direction、name 等上下文，但不得包含 token、环境变量值、剪贴板内容或文件内容。
- `read_process_output`、`interact_with_process`、`force_terminate`、`list_sessions` 直接透传 DesktopCommander 的结果和错误文本；其原始错误文本不属于本冻结文档的稳定错误码集合。
- 错误消息措辞不冻结，但 `<code>` 必须保持本文和第 15 节列出的值。

### 5.2 冻结的 MCP 错误码

| 领域 | 错误码 |
| --- | --- |
| `start_process` 参数 | `invalid_arguments`、`conflicting_timeout`、`invalid_timeout`、`invalid_shell` |
| `start_process` workdir | `workdir_not_found`、`workdir_not_directory`、`workdir_unsupported_character` |
| 进程后端 | `process_backend_unavailable` |
| workspace 文件工具 | `invalid_arguments`、`invalid_path`、`path_escape`、`not_found`、`not_a_file`、`not_a_directory`、`binary_file`、`file_too_large`、`permission_denied`、`io_error`、`invalid_text`、`content_too_large`、`parent_not_found` |
| staging MCP | `invalid_arguments`、`invalid_direction`、`invalid_name`、`not_found`、`not_a_file`、`permission_denied`、`io_error` |
| 剪贴板 | `invalid_arguments`、`invalid_text`、`clipboard_too_large`、`clipboard_unavailable`、`clipboard_error` |
| 系统信息兜底 | `system_error` |

补充语义：

- `start_process` 校验失败固定使用 `invalid_arguments`、`conflicting_timeout`、`invalid_timeout`、`invalid_shell`、`workdir_*`、`process_backend_unavailable`。
- `path_escape` 表示路径解析后离开 workspace，或被拒绝的绝对路径、drive-relative 路径、symlink 逃逸。
- `binary_file` 表示内容不是有效 UTF-8 文本或包含 NUL 字节。
- `not_a_file` 与 `not_a_directory` 分别表示目标类型不符合操作要求。
- `clipboard_unavailable` 表示平台没有可用剪贴板后端；`clipboard_error` 表示后端存在但调用失败或输出解码失败。

## 6. `start_process` 冻结语义

### 6.1 workdir

- 默认 workdir 是 bridge workspace（`BRIDGE_WORKSPACE`，默认 `<data>/workspace`）。
- 相对路径相对默认 workdir 解析；允许 `..` 离开默认 workdir，也允许绝对路径。
- workdir 必须已存在且是目录；指定 workdir 不会被自动创建。
- workdir 不受 workspace 路径限制，因为 shell 本身拥有 bridge 用户的完整权限。
- 空字符串、非 string、含 NUL 的 workdir 返回 `invalid_arguments`；不存在返回 `workdir_not_found`；存在但不是目录返回 `workdir_not_directory`。
- 每次调用独立拼接 workdir 前缀，不依赖跨调用的 `cd` 状态。

### 6.2 timeout / timeout_ms

- `timeout` 是单次调用等待初始输出的时间，不是杀进程超时。
- 默认值 30000ms；取值范围 1..3600000 的安全整数；越界或非整数返回 `invalid_timeout`。
- `timeout` 与 `timeout_ms` 按“字段是否存在”互斥；同时出现即返回 `conflicting_timeout`，即使值相同。
- 超时后进程继续作为 session 运行，调用方应使用 `read_process_output` 继续读取。
- v1 已知限制：DesktopCommander 单次实际等待约 50 秒；大于该值的 `timeout` 仍被接受，但不会延长单次等待。

### 6.3 shell

- `command` 必须是非空 string；bridge 不做用途识别，原样交给选定的 shell。
- `shell` 只接受当前平台检测到的别名，大小写不敏感，trim 后匹配；不接受绝对路径、可执行文件名或任意字符串。
- Windows 别名：`powershell`、`pwsh`、`cmd`、`git-bash`。裸 `bash`/`bash.exe` 明确拒绝并返回 `invalid_shell`。
- Unix 别名：`bash`、`zsh`、`sh`、`fish`。
- 默认 shell：Windows 依次选择 `powershell`、`pwsh`、`cmd`；Unix 优先 `$SHELL` 对应的可用别名，否则 `bash`，再否则 `sh`。`BRIDGE_SHELL` 可覆盖默认值，但必须是可用别名。
- shell 别名必须解析到对应可执行文件，并保证 workdir 引用和命令传递正确；具体如何传递给后端属于内部实现，不属于冻结行为。
- workdir 通过命令前缀实现：PowerShell 使用 `Set-Location`；`cmd` 使用 `cd /d`；POSIX 系列使用 `cd`；`fish` 使用 `cd ...; and`；`git-bash` 先转换为 MSYS 路径形式再引用。
- `cmd` 下 workdir 含 `%` 返回 `workdir_unsupported_character`，避免变量展开。
- 引用规则：PowerShell 单引号并将内部 `'` 替换为 `''`；POSIX 单引号并将内部 `'` 替换为 `'\''`；`cmd` 使用双引号。

### 6.4 返回

- 成功时返回进程输出文本，并保留 `isError` 和 `structuredContent`（若存在）。
- v1 兼容要求是保留客户端依赖的文本状态标记（PID、started、finished、waiting、termination）；不要求复刻某个实现的完整措辞或 emoji。
- 不额外包裹 JSON，不注入 bridge 元数据。
- `start_process` 的校验错误使用第 5.2 节错误码和统一错误格式。

## 7. 进程会话与终止语义

- `start_process` 后仍在运行的进程形成一个 session；快速完成的一命令调用直接返回完成文本。
- session 至少在以下方面保持稳定语义：
  - `read_process_output` 可按 `pid` 继续读取输出，支持从上次读取后新增、绝对行偏移和末尾 tail 的 `offset` 语义。
  - `interact_with_process` 可向同一 `pid` 发送文本并返回新输出；没有 TTY 语义。
  - `list_sessions` 列出当前活跃 session；已终止的 session 不再列出。
  - `force_terminate` 终止 session 及其进程树；终止后该 session 不再可用。
- 单次等待超时不会终止 session；session 可能因进程自然退出、`force_terminate`、bridge 停止或后端退出而结束。
- v1 不保证 session 跨 bridge 重启或跨 MCP 会话持久；调用方不应把 session 当作持久任务队列。
- v1 不提供 PTY、终端尺寸、ANSI 终端模拟或结构化 exit code；这些属于已知限制。

## 8. `get_system_info` 冻结形状

`get_system_info` 无参数，返回 2 空格缩进的 JSON 文本，顶层必须包含以下字段：

```json
{
  "os": { "platform": "...", "type": "...", "release": "...", "version": "..." },
  "hostname": "...",
  "arch": "...",
  "shell": { "default": "...", "available": ["..."], "pathStyle": "windows|posix" },
  "paths": {
    "defaultWorkdir": "...",
    "workspaceRoot": "...",
    "dataDir": "...",
    "staging": { "inbox": "...", "outbox": "..." }
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
    "clipboard": { "available": true, "backend": "powershell", "reason": null }
  }
}
```

冻结规则：

- `os.platform` 使用运行时平台标识；`os.type`、`os.release`、`os.version` 来自对应系统调用。
- `shell.available` 是当前实际检测到的别名列表；`shell.default` 必须属于该列表。
- `shell.pathStyle` 在 Windows 为 `"windows"`，其他平台为 `"posix"`。
- `paths.defaultWorkdir` 与 `paths.workspaceRoot` 在 v1 相等，但两个字段都必须存在。
- `paths.dataDir`、`paths.staging.inbox`、`paths.staging.outbox` 必须返回绝对路径。
- `fileService.enabled=false` 时 `baseUrl` 必须为 `null`；`host` 和 `port` 仍返回。
- `fileService.enabled=true` 时 `baseUrl` 是回环地址，不是 Tailscale 地址。
- `capabilities.screenshot` 和 `capabilities.clipboard` 只表示能力检测结果，不代表当前一定可成功执行；不可用时 `reason` 为非空字符串。
- 不得返回：设备或硬件标识、用途专用清单、开发工具/运行时探测集合、环境变量、token、剪贴板内容、进程列表、网络接口列表。
- 允许在不改变上述字段语义的前提下追加只读信息字段（见第 17 节）。

## 9. workspace 文件工具冻结语义

所有 workspace 文件工具只允许访问 `BRIDGE_WORKSPACE` 内。该限制用于降低误操作和路径错误，不是对抗 MCP 调用者的安全边界；`start_process` 不受此限制。

共同规则：

- `path` 必须是非空 string、不含 NUL；Windows 接受 `/` 和 `\`，POSIX 只接受 `/`。
- 拒绝任何 `..` 路径段；拒绝逃出 workspace 的绝对路径、drive-relative 路径和 symlink。
- 相对路径相对 workspace 根解析；绝对路径原样参与解析。
- 读、列目录、stat 解析 symlink 后仍须位于 workspace 内，否则 `path_escape`。
- 写入使用同目录临时文件 + `rename`，不跟随最终组件的 symlink；目标已存在且为目录返回 `not_a_file`。
- 只支持 UTF-8 文本。读取时严格解码并拒绝 NUL；去掉开头 BOM；CRLF/CR 规范化为 LF。写入不自动添加 BOM。
- 所有 JSON 结果使用 2 空格缩进。
- 文本上限：单次读取结果和写入内容各 4 MiB；源文件 32 MiB；单次最多 5000 行，默认 2000 行；目录最多 1000 条。

### 9.1 `read_file`

- 参数：`path`（语义必填）、`offset`（默认 1，1-based）、`limit`（默认 2000，1..5000）。
- 返回字段：`path`、`absolutePath`、`offset`、`limit`、`totalLines`、`returnedLines`、`truncated`、`truncatedByBytes`、`truncatedLine`、`content`。
- 空文件行数为 0；尾随换行不额外产生空行。
- `offset` 超出范围时返回空 `content`、`returnedLines: 0`、`truncated: false`。
- 选中内容超过 4 MiB 时在最后一条完整行处截断；第一条选中行本身超限时返回该行前 4 MiB 并设置 `truncatedLine: true`。
- 目录返回 `not_a_file`；不存在返回 `not_found`；源文件超过 32 MiB 返回 `file_too_large`。

### 9.2 `write_file`

- 参数：`path`（语义必填）、`content`（语义必填）、`create_dirs`（默认 false）。
- 返回字段：`path`、`absolutePath`、`bytes`、`created`、`replaced`。
- 内容含 NUL 返回 `invalid_text`；UTF-8 编码后超过 4 MiB 返回 `content_too_large`。
- 父目录不存在且 `create_dirs=false` 返回 `parent_not_found`；目标已存在且是目录返回 `not_a_file`。
- 覆盖已有普通文件或 symlink 时，symlink 被替换为普通文件，不跟随目标写入。

### 9.3 `list_directory`

- 参数：`path`（默认 `"."`），非递归。
- 返回字段：`path`、`absolutePath`、`entries`、`total`、`truncated`。
- 条目字段：`name`、`type`（`file`/`directory`/`symlink`/`other`）、`size`、`modifiedAt`；目录条目 `size`/`modifiedAt` 可为 `null`。
- 排序：目录优先，然后名称大小写不敏感升序。
- 超过 1000 条时截断并设置 `truncated: true`；`total` 是截断前总数。
- 不存在返回 `not_found`；不是目录返回 `not_a_directory`。

### 9.4 `stat_file`

- 参数：`path`（语义必填）。
- 返回字段：`path`、`absolutePath`、`realPath`、`type`、`size`、`isSymlink`、`createdAt`、`modifiedAt`。
- workspace 内 symlink 允许，返回目标类型、`isSymlink: true` 和解析后的 `realPath`；目标在 workspace 外返回 `path_escape`。
- 不存在返回 `not_found`。

## 10. staging MCP 冻结语义

### 10.1 文件名规则

- 解码后长度 1..128 个 Unicode code point。
- 正则：`^[\p{L}\p{N}][\p{L}\p{N}._-]*$`。
- 不允许空格、路径分隔符、控制字符、`%`、`:`、`*`、`?`、`"`、`<`、`>`、`|`。
- 不允许以 `.` 结尾；不允许以 `.part` 结尾（大小写不敏感）。
- 不允许 Windows 保留名（大小写不敏感，去掉扩展名后判断）。
- HTTP 路径段只解码一次；解码失败、解码后含 `/` 或 `\`、或多段路径一律拒绝。
- 由 shell 创建的不合法名称可以存在于 staging，但不会出现在 list 结果中，也不能通过 MCP/HTTP 删除。

### 10.2 `list_staged_files`

- 参数：`direction`（语义必填，`inbox` 或 `outbox`）；其他值返回 `invalid_direction`。
- 返回字段：`direction`、`path`、`files`、`totalBytes`、`ignoredInvalid`、`truncated`。
- `files` 条目：`name`、`size`、`modifiedAt`。
- 只列出合法名称的普通文件；symlink、目录、`.part` 跳过；不合法名称计入 `ignoredInvalid`。
- 按名称升序；最多 10000 条；不返回 token、URL 或内容。
- 文件服务禁用时仍可工作。

### 10.3 `delete_staged_file`

- 参数：`direction`（语义必填）、`name`（语义必填）。
- 返回字段：`deleted`、`direction`、`name`。
- 只允许删除普通文件或 symlink；目录返回 `not_a_file`；不存在返回 `not_found`。
- 删除前校验名称并 `lstat`，不跟随 symlink 目标。
- 文件服务禁用时仍可工作。

## 11. HTTP 文件服务冻结行为

### 11.1 基础

- 仅在文件服务启用时监听；绑定固定为 `127.0.0.1`，默认端口 8766，路径前缀 `/files`。
- 所有 `/files/*` 请求必须带 `Authorization: Bearer <BRIDGE_FILES_TOKEN>`。
- token 比较使用固定时间比较；失败返回 401、`WWW-Authenticate: Bearer realm="bridge-files"` 和 JSON 错误体。
- JSON 响应使用 `Content-Type: application/json; charset=utf-8` 和 `Cache-Control: no-store`；下载响应带 `X-Content-Type-Options: nosniff`。
- 请求日志只记录 method、合法文件名、状态码、字节数和耗时；不记录 Authorization、token、查询字符串或文件内容。

### 11.2 端点

| 方法与路径 | 行为 | 成功状态 |
| --- | --- | --- |
| `PUT /files/inbox/:name` | 流式上传；支持可选 `X-Checksum-SHA256` | 201 新文件；200 覆盖 |
| `GET /files/outbox/:name` | 下载普通文件；不支持 Range/HEAD/压缩 | 200 |
| `GET /files/outbox` | 列出 outbox 合法普通文件 | 200 |
| `DELETE /files/inbox/:name` | 删除普通文件或 symlink | 204 |
| `DELETE /files/outbox/:name` | 删除普通文件或 symlink | 204 |

上传要点：

- `Content-Length` 必须存在；缺失或 chunked 无长度返回 411。
- 超过 `BRIDGE_MAX_FILE_BYTES` 返回 413，不读取 body。
- checksum 头格式非法返回 400；流式计算不匹配返回 422 并删除临时文件。
- 实际接收字节数必须等于 `Content-Length`；不一致返回 400 `length_mismatch` 并删除临时文件。
- 配额预检按 inbox + outbox 普通文件总字节数扣除同名旧文件后计算；超出返回 507。
- 使用同目录 `.part` 临时文件 + `rename`；失败、断开或校验失败时清理 `.part` 并释放预留配额。
- 成功 body：`{"direction":"inbox","name":"<name>","size":123,"sha256":"<hex>","replaced":false}`。

下载要点：

- 只允许普通文件；symlink、目录、`.part` 统一返回 404。
- 响应头包含 `Content-Type: application/octet-stream`、`Content-Length` 和 `Content-Disposition: attachment`。
- 列表响应字段：`direction`、`files`、`totalBytes`、`truncated`；条目字段 `name`、`size`、`modifiedAt`。
- 删除成功无 body；目录返回 400 `not_a_file`；不存在返回 404；不跟随 symlink。

### 11.3 配额、TTL 与清理

- `BRIDGE_STAGING_QUOTA_BYTES` 是 inbox + outbox 普通文件总字节硬上限；symlink 不计入。
- 并发上传通过内存预留字节预检，不得绕过配额。
- staging 普通文件保留 7 天；`<data>/staging` 下超过 1 小时的 `.part` 临时文件清理。
- 启动时和之后每小时执行一次 best-effort 清理；日志只记录数量和字节数。
- 不上传执行、不解析、不自动解压；TTL 在 v1 固定为 7 天。

### 11.4 HTTP 错误码

| 状态 | code |
| --- | --- |
| 400 | `bad_request`、`invalid_name`、`invalid_checksum_header`、`length_mismatch`、`not_a_file` |
| 401 | `unauthorized` |
| 404 | `not_found` |
| 405 | `method_not_allowed` |
| 411 | `length_required` |
| 413 | `file_too_large` |
| 422 | `checksum_mismatch` |
| 500 | `io_error` |
| 507 | `quota_exceeded` |

错误体统一为 `{"error":{"code":"...","message":"..."}}`。

## 12. 文件服务启用/禁用行为

启用（`config.files.enabled=true` / 提供非空文件 token）：

- `filesToken` 必填且必须不同于 MCP token，否则启动失败。
- 必须监听文件端口；监听失败或提前退出时整体启动失败，不得静默降级为禁用。
- `get_system_info.fileService.enabled=true`，`baseUrl` 为回环地址。
- MCP token 访问文件服务必须返回 401。

禁用（`config.files.enabled=false` / 未提供文件 token）：

- 不启动文件服务，不监听文件端口；不得因文件服务不可用而阻止 MCP 启动。
- `get_system_info.fileService.enabled=false`，`baseUrl=null`，`host`/`port` 仍返回。
- `list_staged_files` 与 `delete_staged_file` 仍然可用，因为 staging 目录属于数据目录而非 HTTP 服务。
- 文件服务禁用时不得要求 `BRIDGE_FILES_TOKEN` 环境变量。

## 13. 剪贴板冻结行为

`get_clipboard`：

- 无参数，返回 `{"text": "...", "bytes": <UTF-8 字节数>, "backend": "..."}`。
- 空剪贴板返回 `text: ""`、`bytes: 0`。
- 超过 1 MiB UTF-8 的文本返回 `clipboard_too_large`。
- 后端不可用返回 `clipboard_unavailable`；后端存在但调用失败返回 `clipboard_error`。
- 不得记录或回显剪贴板内容到日志/错误文本。

`set_clipboard`：

- 参数 `text` 必须是 string；空字符串合法并用于清空剪贴板。
- 含 NUL 返回 `invalid_text`；超过 1 MiB 返回 `clipboard_too_large`。
- 返回 `{"ok": true, "bytes": <UTF-8 字节数>, "backend": "..."}`。
- 文本只能通过 stdin 传给后端，不得进入 argv、临时文件或日志。
- 不得回显写入的文本内容。

后端可用性（Windows PowerShell、macOS pbpaste/pbcopy、Linux wl-clipboard/xclip/xsel）属于环境能力，不属于二进制契约的冻结保证；但可用时的文本读写语义必须保持一致。

## 14. 截图冻结行为

- `capture_screenshot` 无参数，截取 bridge 主机桌面，不是调用方工作站。
- 成功时返回 JSON 元数据文本（至少包含尺寸、保存路径、字节数、是否内联）以及可选的 image attachment；完整文件保存到 `<data>/screenshots`，保留 7 天，内联上限 4 MiB。
- 超过内联上限时只返回元数据文本和说明，不返回 image。
- 失败（headless、锁屏、无交互桌面、后端不可用）返回 MCP 错误文本；测试允许按第 19 节跳过。
- 不新增截图参数、不改变“截取 bridge 主机”的语义；返回元数据可以追加字段，但不得移除尺寸/路径/内联状态等既有语义。

## 15. Token 隔离冻结要求

- MCP token 与文件 token 必须是两个不同的非空字符串；文件服务启用时文件 token 必填。
- MCP token 只用于 MCP 认证；不得传给 bridged shell、进程成员、系统成员或文件成员环境。
- 文件 token 只用于文件服务；不得传给 supergateway、MCP 成员或 bridged shell。
- 两个 token 都不得出现在：argv、`supergateway.runtime.json`、`get_system_info` 输出、错误文本、健康检查、诊断信息、事件 payload、日志。
- bridged shell 中 `LAPTOP_MCP_TOKEN`、`BRIDGE_FILES_TOKEN`、`SUPERGATEWAY_API_KEY`、`SUPERGATEWAY_API_KEY_FILE` 必须为空或不存在。
- 用 MCP token 访问文件服务必须返回 401；用文件 token 访问 MCP 必须返回 401。
- `Bridge` 对象的 `config`、`health()`、`diagnostics()`、事件和日志不得包含任一 token。

## 16. 非冻结内容

以下内容可以变化，只要不违反本文行为：

- MCP 工具的 `description`、instructions、帮助文本、emoji 和文档措辞。
- PID 的具体数值、端口分配结果、启动/停止耗时、超时轮询间隔、清理执行时间。
- 路径分隔符风格、换行符、JSON 空白、日志格式和日志字段文本。
- 内部模块划分、类/函数名、子进程数量与名称、依赖注入方式。
- 实现语言、运行时和打包方式。
- 截图/剪贴板具体后端的选择顺序与“当前是否可用”，但不可用时必须给出明确错误或按测试规则跳过。
- 额外诊断字段、额外日志、额外事件；前提是不泄露 token 且不改变既有事件/错误语义。

## 17. 兼容性策略

允许的加法变化（仍属 `bridge-v1`）：

- 在工具结果中增加 `structuredContent` 或额外只读字段。
- 通过可选参数或新的能力协商提供 PTY、终端尺寸或交互增强，且不改变既有参数/状态的语义。
- 在进程完成结果中附加 `exitCode` 或等价结构化字段；文本结果仍需保持可用。
- 在 `get_system_info` 中追加只读信息字段。
- 在 HTTP 响应中追加字段或响应头。
- 改进错误消息文本、日志和内部实现。

不允许的破坏性变化（需要 `bridge-v2`）：

- 重命名、删除或新增 v1 的 15 个工具，改变工具前缀策略。
- 把既有可选参数改为 required，或改变参数类型/含义。
- 删除或重命名本文冻结的错误码；改变错误码的触发条件。
- 改变 `start_process` 的默认 workdir、timeout 语义、shell 白名单或 workdir 解析规则。
- 改变进程 session 的读取、交互或终止语义。
- 改变 `get_system_info` 的既有字段含义或删除字段。
- 改变 workspace 路径限制、UTF-8 文本语义、行计数或文件大小上限。
- 改变 staging 名称规则、list/delete 语义或 HTTP 端点/状态码/认证行为。
- 改变 token 隔离要求或文件服务启用/禁用语义。
- 改变剪贴板/截图的核心语义（纯文本剪贴板、截取 bridge 主机桌面）。

`bridge-v2` 变更必须同时更新客户端、本冻结文档和 snapshot，并保留清晰的迁移说明。

## 18. 已知 v1 限制（不是必须复现的需求）

以下限制记录当前 v1 的已知能力缺口。替换实现可以在兼容前提下改进它们；不得把它们误写成 v1 必须保持的行为：

- DesktopCommander 快速命令只返回文本完成状态，没有稳定的结构化退出码。
- DesktopCommander 单次等待上限约 50 秒；更长的 `timeout` 不会延长单次等待。
- 没有 PTY；需要 TTY 的程序可能无法正常工作。
- 没有结构化 exit code；需要精确退出码时应通过后续兼容扩展引入。
- 截图需要交互式桌面；headless、锁屏或非交互会话可能失败。
- 剪贴板依赖平台后端（Windows PowerShell/pwsh、macOS pbpaste/pbcopy、Linux wl-clipboard/xclip/xsel）；headless 环境可能不可用。
- 文件服务需要部署侧手动添加 Tailscale Serve 8443 路由并放行 ACL；bridge 只返回回环 `baseUrl`。
- 文件服务不做 TLS、Range、断点续传、分块上传、压缩、CORS、多目录或多用户。
- workspace 文件工具只支持 UTF-8 文本，且不是安全沙箱。
- 健康检查只检查 gateway；`/healthz` 成功不代表进程后端已初始化。

这些限制可能在 v1 内以加法方式改善，例如增加结构化退出码字段或 PTY 能力；改善时不得破坏既有工具名、输入、错误码和语义状态。

## 19. 一致性验证程序

替换后端、重构实现或升级依赖前后，必须在 bridge 源码仓库运行以下三条命令：

```bash
node scripts/smoke-test.mjs
node scripts/bridge-object-test.mjs
node scripts/tools-snapshot.mjs --check
```

覆盖范围：

| 命令 | 覆盖 |
| --- | --- |
| `node scripts/tools-snapshot.mjs --check` | MCP 工具名、title、annotations、输入 schema；15 个工具且仅 15 个 |
| `node scripts/bridge-object-test.mjs` | `createBridge` 生命周期、ephemeral port、健康检查、token 脱敏、start/stop 幂等、文件服务启用/禁用 |
| `node scripts/smoke-test.mjs` | 端到端 MCP/HTTP 行为：认证、进程 workdir/timeout/shell、系统信息、workspace 文件、staging、HTTP 文件服务、token 隔离、剪贴板、截图 |

要求：

- 三条命令全部通过（退出码 0），才能替换后端或合入影响行为的改动。
- 无法在本机运行某条命令时，替换实现必须提供等价的**黑盒**一致性验证，覆盖同一工具面、输入、错误码、进程会话、文件服务与 token 隔离；不能用内部单元测试替代。
- snapshot 只能在批准的 v1 加法变更后更新；更新必须审查并随变更一起提交。
- 测试运行必须使用临时数据目录、临时 workspace、`port: 0` 和随机 token；不得依赖真实 `.env`、`BRIDGE_FILES_TOKEN` 或已在运行的 CLI bridge。

允许的跳过（必须打印原因，不能静默跳过）：

- 截图：当前环境没有交互式桌面。
- 剪贴板：当前平台没有可用剪贴板后端。
- symlink 逃逸测试：当前操作系统或权限无法创建 symlink。

任何其他失败都必须视为一致性失败，不得归类为跳过。

## 20. 变更控制

- 本冻结文档和 `specs/bridge-tools-v1.snapshot.json` 是 `bridge-v1` 的发布基线。
- 修改工具面、输入 schema、错误码、进程/文件/HTTP 语义时，必须先判断是加法兼容还是破坏性变更。
- 加法兼容变更需要：更新实现、更新 snapshot（如涉及工具面）、保留三条一致性命令通过、在本文件记录变更。
- 破坏性变更需要：升级契约版本到 `bridge-v2`、新增冻结文档、更新客户端并保留适配期。
- 不能仅通过修改测试或 snapshot 来“修复”行为回归；测试和 snapshot 的变更必须反映有意的契约变更。
