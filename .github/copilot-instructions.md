## 快速说明 — ai-cli 项目

以下说明面向要在此仓库编写或修改代码的 AI 助手（Copilot / agents）。内容聚焦于本项目的实际实现细节、约定与常见陷阱，便于快速上手并保持行为一致性。

### 项目概览（大局）
- 单文件小型 Go CLI：核心实现位于 `main.go`。模块名在 `go.mod`（Go 1.24.3）。
- 用途：向 OpenAI 兼容的 API 发起聊天补全请求，支持流式输出（SSE 风格），并把 AI 回复以纯文本打印到 stdout。
- 主要依赖：`github.com/spf13/cobra` 用于 CLI 子命令和标志。

### 关键文件
- `main.go` — 全部业务逻辑：配置加载/创建、命令解析（`ai-cli [prompt]`）、子命令 `model`, `add`, `remove`、构建并发送 HTTP 请求到 `cfg.BaseURL + "/chat/completions"`，以及流式响应解析。
- `go.mod` — 指明 Go 版本和依赖（cobra v1.10.1）。

### 如何构建 / 运行（开发者工作流）
- 构建可执行文件：在仓库根目录运行 `go build -o ai-cli .`
- 直接运行（无构建产物）：`go run main.go <prompt>` 或从 stdin：`echo "ls -la" | go run main.go`
- 运行二进制：`./ai-cli "What is the best way to..."`

注意：项目依赖 Go >= 1.24（见 `go.mod`）。

### 配置和运行时约定（非常重要）
- 配置文件位置：`~/.ai_cli_config`（`configName` 常量）。AI 助手在修改配置相关代码时不可更改此路径，除非同时更新所有引用。
- 必需字段：`api_key`（缺失或保留占位符 `YOUR_API_KEY_HERE` 会导致程序在 `loadConfig()` 中报错并退出）。
- 可用字段：`base_url`, `default_model`, `models`（逗号分隔）, `request_timeout`, `system_prompt`, `proxy_url`。
- 默认配置创建：若不存在配置文件，`createDefaultConfig()` 会写入默认内容并直接调用 `os.Exit(1)`；因此任何修改该函数要注意保留该行为或显式处理退出逻辑。
- 保存行为：`saveConfig()` 试图保留配置文件的其他行，只替换或追加 `default_model` 与 `models`，不要改动该函数的行替换策略，除非你理解文本替换的副作用。

### 网络与 API 约定
- 目标端点：`POST {BaseURL}/chat/completions`，请求体使用结构体 `APIRequest{Model, Messages, Stream}`。
- 流式处理：程序按行读取响应体，期望 SSE 风格行以 `data: ` 前缀发送片段，遇到 `data: [DONE]` 停止。修改流式解析时请保留对 `data: ` 前缀和 `[DONE]` 的处理。
- 身份验证：使用 `Authorization: Bearer {APIKey}`。
- 代理支持：若 `proxy_url` 非空，会把 `http.Client.Transport` 设置为 `http.ProxyURL(proxy)`。

### CLI 行为与子命令
- 主命令：`ai-cli [prompt]`，若无参数从 stdin 读取完整 prompt。
- 子命令说明：
  - `ai-cli model [name]`：无参列出并交互选择；有参则设置默认模型（必须在 `models` 列表中）。
  - `ai-cli add <model>`：向 `models` 列表添加项并保存配置。
  - `ai-cli remove`：交互式按编号删除模型（并可能更新 `default_model`）。

### 项目约定与禁忌（为 AI 变更提供指导）
- 输出风格：默认 system prompt 指定“Plain text（无 Markdown）”和简洁命令输出。任何对默认 system prompt 的修改会影响 AI 输出格式，谨慎调整。
- 当更改模型名称或默认模型相关逻辑时，请同时更新 `createDefaultConfig()` 中的 `models` 列表以保持一致性。
- 错误处理：当前代码在遇到配置或 HTTP 错误时常使用 `log.Fatalf`（会退出进程）。当你修改这些点，注意不要破坏 CLI 的 UX（退出码、提示信息）。

### 代码位置示例（便于编辑时直接查看）
- 流式解析：`callAPI()` 中的 scanner 循环 — 查找 `strings.HasPrefix(line, "data: ")`。
- 配置解析：`loadConfig()`（逐行读取并用 `key = value` 解析）。
- 配置写回：`saveConfig()`（只替换或追加 `default_model` 与 `models`）。

### 编辑和 PR 检查清单（给 AI 及人类审阅者）
1. 保持 `~/.ai_cli_config` 的字段语义与现有解析兼容。
2. 不破坏 SSE 风格的流式解析；若改变格式，更新 README 与提示文本。
3. 变更默认 system prompt 时，说明为什么要改（影响所有 CLI 输出）。
4. 运行 `go build` 确保编译通过；目标 Go 版本参考 `go.mod`。

如果有任何不清楚或想要更多示例的部分，请告诉我；我可以把常见用例、运行示例或更多代码片段补充到此文件中。
