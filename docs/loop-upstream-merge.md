# loop 分支上游合并维护指南

本文档用于长期维护 `loop` 分支：以后将 upstream/main 合并到 `loop` 时，可以把下面的 Prompt 直接交给 AI，要求它在解决冲突时保留 Loop Desktop 嵌入 Crush 所需的改造。

## loop 分支的核心目标

`loop` 分支用于让 Crush 可以作为 Loop Desktop 的内置 agent runtime 以 in-process 方式嵌入。为此，不能让多个 Crush app/workspace 共用原先的包级全局状态；每个 App 实例必须拥有自己的运行态对象。

当前 loop 分支维护的关键差异：

1. MCP 状态实例化
   - `internal/agent/tools/mcp.Manager` 持有 MCP sessions、states、tools、prompts、resources、event broker、init channel。
   - `mcp.NewManager()` 用于每个 App/workspace 创建独立 MCP 管理器。
   - 原有包级函数（如 `mcp.Initialize`、`mcp.GetStates`、`mcp.Tools`）必须保留，并委托给 `mcp.DefaultManager()`，保证 upstream CLI 代码兼容。

2. Background shell 状态实例化
   - `shell.NewBackgroundShellManager()` 创建独立后台 shell 管理器。
   - `BackgroundShellManager` 自己持有 shell map 和 ID counter，避免不同嵌入实例共享 background job。
   - 原有 `shell.GetBackgroundShellManager()` 作为兼容默认 singleton 保留。

3. LSP 事件状态实例化
   - `app.LSPEventManager` 持有 LSP states 和 broker。
   - App 内部通过 `app.LSPEvents` 更新和订阅 LSP 状态。
   - 原有 `app.GetLSPStates()`、`app.SubscribeLSPEvents()` 等默认 wrapper 保留。

4. App/Agent/Workspace 传递实例状态
   - `app.App` 持有 `MCPManager`、`ShellManager`、`LSPEvents`。
   - `agent.NewCoordinator` / `SessionAgentOptions` 接收并向 tools/session agent 传递 MCP/Shell manager。
   - MCP tools、resource tools、crush_info、bash/job tools 优先使用传入的 manager。
   - `workspace.AppWorkspace` 和 `backend.Workspace` 的 MCP/LSP 操作必须读取对应 workspace 的 manager，不能回退到包级全局状态。

## 合并上游时最容易冲突的文件

重点检查这些文件。如果 upstream 改了相同区域，解决冲突时以“保留实例化 manager + 保留默认 wrapper 兼容”为准：

- `internal/agent/tools/mcp/init.go`
- `internal/agent/tools/mcp/tools.go`
- `internal/agent/tools/mcp/prompts.go`
- `internal/agent/tools/mcp/resources.go`
- `internal/agent/tools/mcp/init_test.go`
- `internal/shell/background.go`
- `internal/app/app.go`
- `internal/app/lsp_events.go`
- `internal/agent/agent.go`
- `internal/agent/coordinator.go`
- `internal/agent/tools/mcp-tools.go`
- `internal/agent/tools/list_mcp_resources.go`
- `internal/agent/tools/read_mcp_resource.go`
- `internal/agent/tools/crush_info.go`
- `internal/agent/tools/bash.go`
- `internal/agent/tools/job_output.go`
- `internal/agent/tools/job_kill.go`
- `internal/backend/events.go`
- `internal/backend/config.go`
- `internal/workspace/app_workspace.go`
- `internal/commands/commands.go`
- `internal/ui/completions/completions.go`
- `internal/ui/model/ui.go`
- `internal/ui/model/lsp.go`

## 给 AI 的上游合并 Prompt

```text
你正在维护 github.com/charmbracelet/crush 的长期分支 `loop`。这个分支会定期合并 upstream/main，但必须保留 Loop Desktop 嵌入 Crush 所需的 in-process 多实例隔离改造。

合并目标：把 upstream/main 的新代码合入当前 `loop` 分支，同时保留并修复所有 per-App/per-workspace state split。

必须遵守：
1. 不要把 MCP、background shell、LSP 事件状态重新合并回包级全局变量。
2. `internal/agent/tools/mcp.Manager` 必须继续拥有 sessions/states/tools/prompts/resources/event broker/init state；每个 App 使用 `mcp.NewManager()`。
3. 旧的包级 MCP API 必须保留为兼容 wrapper，并委托给 `mcp.DefaultManager()`，除非所有 upstream 调用点都已经迁移且确认 CLI 兼容不需要它们。
4. `shell.BackgroundShellManager` 必须可以通过 `shell.NewBackgroundShellManager()` 创建独立实例，且 ID counter 不能再是跨实例全局共享状态。`shell.GetBackgroundShellManager()` 仅作为 legacy default wrapper。
5. `app.App` 必须继续持有 `MCPManager`、`ShellManager`、`LSPEvents`，并将这些实例传给 agent coordinator、tools、workspace/backend 方法。
6. `workspace.AppWorkspace` 和 `backend.Workspace` 中的 MCP/LSP 查询、刷新、Docker MCP 启停、resource/prompt 读取必须使用当前 workspace/app 的 manager，不要调用默认全局 wrapper。
7. 如果 upstream 新增了 MCP/shell/LSP 相关调用点，要优先添加 WithManager/manager 参数；只有 CLI legacy fallback 才可以使用 default manager。
8. 解决冲突后运行 `gofmt`，并至少执行：
   - `go test ./internal/agent/tools/mcp`
   - `go test ./internal/app ./internal/agent ./internal/agent/tools ./internal/workspace ./internal/backend ./internal/ui/model ./internal/ui/completions`
   - 如果时间允许，执行 `go test ./...`
9. 合并完成后，检查 `rg "mcp\\.(GetStates|Tools|RunTool|ListResources|ReadResource|RefreshTools|RefreshPrompts|RefreshResources|Initialize|InitializeSingle|DisableSingle|Close|SubscribeEvents|WaitForInit|Prompts|Resources|GetPromptMessages)\\(" internal -g '*.go'`：除 legacy fallback 或明确不属于 app/workspace 的默认 CLI 路径外，不应出现直接使用 default manager 的新增业务调用。
10. 最终汇报时列出：冲突文件、保留的 loop 分支改造点、测试命令和结果、仍需人工关注的 upstream 行为变化。
```

## 人工 Review Checklist

合并后人工重点看：

- 是否存在新的包级 `var` 保存 MCP/LSP/tool/session/shell 运行态。
- 是否有新的 tool 构造函数绕过 manager，直接调用 `mcp.*` 或 `shell.GetBackgroundShellManager()`。
- 是否有新 UI/backend/workspace 路径绕过当前 workspace 的 manager。
- App shutdown 是否只清理当前 App 的 MCP clients / background shells / LSP clients。
- 默认 wrapper 是否仍能让 upstream CLI 的原有调用编译通过。
