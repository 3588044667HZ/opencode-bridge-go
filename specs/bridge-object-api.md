# Bridge Object API v1

- 状态：Implemented（Node 首个实现已落地并通过 object/conformance 测试）
- 日期：2026-11-04
- 目标：把当前一堆脚本收敛成一个可实例化、可启动/停止、可黑盒测试的 Bridge 对象；行为契约与 Node.js 实现解耦，便于之后用编译语言重写。

## 1. 设计原则

1. **对象是运行时，不是 CLI**：CLI 只负责加载 `.env`、构造配置、调用对象、打印日志和处理信号。
2. **黑盒可测**：测试只通过 `createBridge` + MCP/HTTP 调用，不 import 内部模块。
3. **秘密与配置分离**：`BridgeConfig` 不含 token；`BridgeSecrets` 单独传入，且绝不会出现在 `config`、`health`、`diagnostics` 或日志中。
4. **端口可 ephemeral**：`port: 0` 表示由 OS 分配，启动后从 `addresses` 读取实际端口，避免测试抢端口。
5. **不假设语言**：API 只使用 JSON 可表达的类型、Promise/等价异步、回调和事件；Node 只是第一个实现。
6. **不假设用途**：仍然是通用 shell + 桌面/文件能力，不添加领域工具。

## 2. 版本

```ts
const BRIDGE_OBJECT_API_VERSION = 1
const BRIDGE_CONTRACT_VERSION = "bridge-v1"
```

破坏性变化升 API/contract 版本。

## 3. 类型

```ts
type BridgeState = "new" | "starting" | "running" | "stopping" | "stopped" | "failed"

type BridgeLogLevel = "debug" | "info" | "warn" | "error"
type BridgeLog = (
  level: BridgeLogLevel,
  message: string,
  fields?: Record<string, unknown>,
) => void

interface BridgeConfig {
  version: 1
  root: string
  dataDir: string
  workspace: string
  mcp: {
    host: "127.0.0.1"
    port: number        // 0 = ephemeral
    path: "/mcp"
  }
  files: {
    enabled: boolean
    host: "127.0.0.1"
    port: number        // 0 = ephemeral; ignored when disabled
    path: "/files"
    maxFileBytes: number
    stagingQuotaBytes: number
  }
  screenshot: {
    backend: "auto" | "ffmpeg" | "powershell"
  }
  shell: {
    default?: string    // optional requested alias; detection still validates it
  }
}

interface BridgeSecrets {
  mcpToken: string
  filesToken?: string   // required iff config.files.enabled
}

interface BridgeOptions {
  config: BridgeConfig
  secrets: BridgeSecrets
  env: Record<string, string>   // explicit; object must not read process.env internally
  logger?: BridgeLog            // default: no-op
}

interface BridgeAddresses {
  mcp: { host: string; port: number; path: string }
  files: { host: string; port: number; path: string } | null
}

interface PublicBridgeConfig {
  version: 1
  root: string
  dataDir: string
  workspace: string
  mcp: BridgeConfig["mcp"]
  files: Omit<BridgeConfig["files"], "enabled"> & { enabled: boolean }
  screenshot: BridgeConfig["screenshot"]
  shell: BridgeConfig["shell"]
}
```

`Bridge` 不返回 `BridgeConfig` 原对象；`config` 是脱敏后的 `PublicBridgeConfig`。

## 4. 工厂与对象

```ts
function createBridge(options: BridgeOptions): Bridge

interface Bridge {
  readonly apiVersion: 1
  readonly contractVersion: "bridge-v1"
  readonly state: BridgeState
  readonly config: PublicBridgeConfig
  readonly expectedTools: readonly string[]
  readonly addresses: BridgeAddresses | null
  readonly mcpUrl: string | null
  readonly filesUrl: string | null

  start(): Promise<void>
  stop(): Promise<void>
  waitForReady(timeoutMs?: number): Promise<void>
  health(): Promise<BridgeHealth>
  diagnostics(): BridgeDiagnostics
  on(event: BridgeEvent, listener: (payload: unknown) => void): () => void
}
```

### 4.1 start()

- 从 `new`、`stopped`、`failed` 可调用；从 `starting`/`stopping` 返回 `already_started`/`stop_in_progress`。
- 校验 config/secrets；创建目录；启动文件服务（如启用）；启动 MCP gateway 与成员；等待就绪。
- 成功后 `state = "running"`，设置 `addresses`/`mcpUrl`/`filesUrl`，触发 `ready`。
- 失败后 `state = "failed"`，清理已启动资源，触发 `error`，Promise reject。
- 已 `running` 时调用是幂等 no-op。

### 4.2 stop()

- 从 `new` 直接置 `stopped`；从 `running`/`starting`/`failed` 停止所有子资源。
- 成功后 `state = "stopped"`，`addresses`/URLs 置 null，触发 `stopped`。
- 幂等；`stopping` 期间重复调用返回同一个 Promise（实现可缓存）。
- 不删除 workspace、staging、截图等用户数据。

### 4.3 waitForReady()

- 等待 `ready` 或 `error`；超时 reject `timeout`。`start()` 本身已等待就绪，此方法用于外部等待/测试。

### 4.4 health()

```ts
interface BridgeHealth {
  state: BridgeState
  uptimeMs: number
  mcp: { ok: boolean; url: string | null; tools: readonly string[] }
  files: { enabled: boolean; ok: boolean | null; url: string | null }
}
```

- `files.ok` 在禁用时为 `null`；启用但探测失败为 `false`。
- 不返回 token、进程 PID 列表、成员内部细节。

### 4.5 diagnostics()

```ts
interface BridgeDiagnostics {
  state: BridgeState
  pid: number | null
  startedAt: string | null
  uptimeMs: number
  config: PublicBridgeConfig
  addresses: BridgeAddresses | null
  expectedTools: readonly string[]
}
```

- 用于测试/CLI 排查；不得包含 secrets。

### 4.6 事件

```ts
type BridgeEvent = "ready" | "stopped" | "error" | "member-error" | "file-service-error"
```

- `ready`: `{ addresses, mcpUrl, filesUrl }`
- `error`: `{ code, message }`
- `stopped`: `{ reason?: string }`
- `member-error`: `{ member: "process" | "screenshot" | "system" | "files"; code; message }`
- `file-service-error`: `{ code, message }`
- `on()` 返回取消订阅函数；监听器异常不得影响 bridge 生命周期。
- 事件 payload 不含 token、文件内容、命令输出。

## 5. 状态机

```text
new --start--> starting --ok--> running --stop--> stopping --ok--> stopped
                 |                  |                  |
                 +--fail--> failed  +--stop----------+
failed --start--> starting
stopped --start--> starting
```

- `start()` 在 `running` 时幂等；`stop()` 在 `new`/`stopped` 时幂等。
- `port: 0` 的实例 stop 后再 start 会得到新的实际端口。
- 失败重试应先 `stop()` 清理；`start()` 自身也必须尽力清理。

## 6. 配置与秘密

- `createBridge` 不读 `.env`、不读 `process.env`；`env` 由调用方显式传入。
- CLI 负责：加载 `.env`、把 `LAPTOP_MCP_TOKEN`/`BRIDGE_FILES_TOKEN` 等转换成 `BridgeSecrets`，把 `BRIDGE_*` 转换成 `BridgeConfig`。
- `files.enabled = true` 时 `filesToken` 必填且必须不同于 `mcpToken`；否则抛 `invalid_secret`。
- `mcpToken` 必填、非空。
- `files.enabled = false` 时不启动文件服务，`filesUrl = null`，`files.ok = null`。
- 端口：`0` 允许；否则 1..65535；MCP 与文件端口不能相同（两者都非 0 时校验）。
- 路径：`dataDir`/`workspace` 为绝对路径；由 CLI 或调用方规范化。
- `expectedTools` 固定为 15 个：见 `specs/bridge-contract-v1.md`（冻结时写入）。

## 7. 错误模型

```ts
class BridgeError extends Error {
  code:
    | "invalid_config"
    | "invalid_secret"
    | "already_started"
    | "stop_in_progress"
    | "not_started"
    | "start_failed"
    | "stop_failed"
    | "port_in_use"
    | "member_failed"
    | "timeout"
    | "unsupported_platform"
}
```

- `start()`/`stop()` reject `BridgeError`；`health()` 不 throw（失败时返回 `ok: false`）。
- 错误消息可含路径、端口、成员名，不得含 token/命令输出。

## 8. 日志

- 通过 `logger` 回调输出，默认 no-op。
- 日志字段只允许：component、member、port、path、state、durationMs、code。
- 禁止记录 token、Authorization、命令文本、文件内容、剪贴板内容、截图内容。
- CLI adapter 把 logger 接到 stderr 或现有日志文件。

## 9. CLI adapter 职责

`scripts/start-bridge.mjs` 应变成薄适配器：

1. 加载 `.env` 与真实环境变量。
2. 校验/归一化为 `BridgeConfig` + `BridgeSecrets`。
3. 调用 `createBridge`。
4. 用 logger 打印 MCP URL、files URL、workspace、tools、shell。
5. 注册 SIGINT/SIGTERM/SIGHUP，调用 `stop()`，超时后退出。
6. 不包含进程管理、成员启动、文件服务生命周期等 bridge 逻辑。

`scripts/portable-start.mjs` 只负责生成/读取 token、设置 env，然后加载 CLI。

## 10. 黑盒测试规则

测试代码只能：

```ts
const bridge = createBridge({ config, secrets, env })
await bridge.start()
const url = bridge.mcpUrl!
// 使用 MCP SDK/HTTP 调用
await bridge.stop()
```

- 不允许 import 内部模块或调用内部函数。
- 使用 `port: 0`、临时 `dataDir`、受控 `env`。
- 断言 `bridge.expectedTools`、`bridge.addresses`、`health()`。
- 现有 `scripts/smoke-test.mjs` 继续作为 CLI/端到端测试；新增 object 级测试用于快速启动/停止和错误路径。
- 文件服务通过 HTTP 测试；MCP 通过 Streamable HTTP 测试。

## 11. 跨语言映射

- Node：`createBridge` + class/closure
- Go：`NewBridge(config, secrets, env, logger)` -> `*Bridge`，`Start(ctx)`、`Stop(ctx)`、`Health(ctx)`、`Addresses()`、`On(event, handler)`
- Rust：`Bridge::new(config, secrets, env, logger)`、`start().await`、`stop().await`、`health().await`
- 事件和错误码保持同名；HTTP/MCP 行为保持字节级兼容（除允许的描述/日志差异）。

## 12. 非目标

- 不把 Bridge 对象变成通用进程/文件 API；工具面仍是 15 个 MCP 工具。
- 不暴露任意文件系统、设备、串口或用途专用能力。
- 不把 token 放进 config/health/diagnostics/日志。
- 不要求 Bridge 对象理解 OpenCode 或客户端；它只提供 MCP/HTTP 服务。
- v1 不要求 PTY/结构化 exit code；这些是后续可选能力，必须在兼容契约下加法引入。

## 13. API 验收标准

1. 现有 CLI 行为不变，smoke 通过。
2. 新 object 测试能在同一进程内 start/stop 两次（port 0 每次不同端口）。
3. start 失败会清理文件服务/MCP 子进程并进入 `failed`。
4. stop 幂等，且 stop 后 `addresses`/URL 为 null。
5. config/health/diagnostics/日志不含两个 token。
6. `files.enabled=false` 时 filesUrl/文件服务均不存在，health 报告 `ok: null`。
7. `expectedTools` 恰好 15 个；启动后实际 tools/list 与之一致。
8. 测试只通过 MCP/HTTP 黑盒调用，不 import 内部模块。
9. Windows 和 Linux 通过；macOS 按可用性测试。
