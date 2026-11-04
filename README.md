# OpenClaw Native PluginBridge Plugin

This is a normal PluginBridge plugin package. It can be installed by copying the
whole `openclaw` directory into a Prism Hub `plugin_dirs` entry, or by
letting Hub install the packaged version into
`~/.prism/pluginbridge-plugins/openclaw`.

The plugin speaks Prism Hub's JSON-over-stdio PluginBridge protocol on one side
and OpenClaw Gateway WebSocket RPC on the other side.

## 本地问题分析与贡献

仓库默认带有 [`prism-plugin-maintainer`](.agents/skills/prism-plugin-maintainer/SKILL.md)，用于本地分析、修复和验证插件问题，无法解决时整理脱敏 Issue 草稿。支持 `.agents/skills` 的智能体可发现它，其他客户端可要求智能体直接阅读该文件。默认不发布；先准备 diff、检查结果、目标和正文供审阅，用户可一次授权 commit、推送与 PR 整条流程，或明确授权无法解决时自动提交 Issue。范围内已有授权不重复询问。公开提交和数据处理说明见 [SDK README](https://github.com/Rokid-Prism/prism-plugin-sdk#本地问题分析与贡献-skill)。

Its Prism runtime mode is `native`. `gateway` is only the underlying OpenClaw
transport and is not exposed as a user-selectable Prism runtime.

Capabilities:

- Forward: create/resume a native-visible OpenClaw Gateway session, send Prism
  messages into it, subscribe run/message progress, and verify visibility via
  chat history.
- Reverse: expose `ListSessions` / `AttachSession` / `Subscribe` so Hub can
  synchronize the Gateway session directory and activity through one plugin-wide
  subscription rather than one WebSocket per conversation.
- Controls: read and switch model/reasoning, report context usage, interrupt,
  compact, rename, pin, archive, delete, and resolve native exec/plugin approval
  requests. Permission switching and queue controls are not published because
  the installed Gateway does not expose a stable per-session permission state
  or queue contract.
- Attachments: pass Hub-materialized files to `chat.send` as base64 payloads.
  Prism rejects files above OpenClaw's default 20 MiB media limit before reading
  them; the Gateway remains authoritative for configured and media-type limits.

Product note:

- `ListSessions`, `AttachSession`, `Subscribe`, `WaitForRun`, and visibility
  verification are the minimum set for a full mobile/glasses remote-conversation
  plugin.
- Reverse sync is useful but not the key gate.
- Gateway-connected TUI, Control UI, channels, and Prism are concurrent peers;
  the Gateway broadcasts live session/message/run/approval events and keeps one
  authoritative history. Embedded local commands such as `tui --local`,
  `chat`, or `terminal` bypass that live bus and are not a full realtime path.

Official direction:

- keep `openclaw` as the formal protocol-native sample path
- do not migrate OpenClaw to desktop automation
- keep extending native history/message query integration instead of changing the integration mode

Repo development only:

```sh
go test ./...
go build -o ./bin/prism-plugin-openclaw ./cmd/openclaw-adapter
```

The repository validates OpenClaw directly with Go tests; release packaging builds `./bin/prism-plugin-openclaw` from `cmd/openclaw-adapter`.
The runtime launcher never compiles source on the user's machine. It starts the
packaged binary directly and fails explicitly if that artifact is missing;
developers must run the build command above after changing Go source.

For normal product usage, plugin install / remove goes through Prism Desktop, not the public CLI surface.

Windows 配置：显式 `OPENCLAW_GATEWAY_URL` 优先，其次是 `OPENCLAW_GATEWAY_PORT`，
最后从当前 OpenClaw 配置读取端口（支持 JSON5）。`OPENCLAW_CONFIG_PATH` 指定配置文件，
`OPENCLAW_STATE_DIR` 指定状态目录；否则使用 `OPENCLAW_HOME` 或系统用户目录下的
`.openclaw`，设置 `OPENCLAW_PROFILE` 时使用 `.openclaw-<profile>`。
无法解析用户目录时仍尝试默认端口；此时需要 `OPENCLAW_GATEWAY_TOKEN`。
`--home` 仍可显式指定包含 `openclaw.json` 的状态目录。
模型和 MiniMax 等供应商凭据由 OpenClaw Gateway 配置；此插件读取 Gateway 的模型列表，
不把供应商 API key 当作 `OPENCLAW_GATEWAY_TOKEN`（后者是本地 Gateway 的连接凭据）。

隔离验证可运行 `PRISM_OPENCLAW_MESSAGE_E2E=1 go test ./openclaw -run '^TestLocalGatewayMinimalMessageE2E$' -v`，
通过环境传入 `OPENCLAW_GATEWAY_URL` 和 `OPENCLAW_GATEWAY_TOKEN`。该用例只发送一条最小消息，
并要求原生历史中出现模型的 assistant 回复。Windows 已准备隔离安装与配置时，可在包含
`MINIMAX_API_KEY` 的 PowerShell 会话中点源 `scripts/start-local-validation.ps1`，再从同一会话
启动 Hub；脚本生成的 Gateway token 只保留在进程环境，不写入配置。

The package is intentionally independent from Prism Hub internals. It imports
the public Go module `github.com/Prism-Rokid/prism-plugin-sdk`; forks should
use that released module rather than a local `replace` directive.
