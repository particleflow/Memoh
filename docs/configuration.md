# Configuration

> Moved from `AGENTS.md` to keep the always-on project rules under the context injection limit. `AGENTS.md` links here where this material is needed.

## Configuration

The main configuration file is `config.toml` (copied from `conf/app.example.toml` or environment-specific templates for development), containing:

- `[log]` — Logging configuration (level, format)
- `[server]` — HTTP listen address and optional `public_url` for the externally reachable Web origin (for example, `https://app.example.com`). Set `MEMOH_SERVER_PUBLIC_URL` to override it. Capability tools use this base URL for credential setup links and the MCP OAuth callback at `/api/oauth/mcp/callback`. Configure it for channel conversations; without it, setup links are relative and OAuth keeps the existing local callback default. Do not include credentials, query parameters, or a fragment.
- `[admin]` — Admin account credentials
- `[auth]` — JWT authentication settings
- `[database]` — Database backend selection (`postgres`)
- `[container]` — Workspace container backend selection (`docker`, `containerd`, `apple`) and common workspace image/data/bridge/CNI settings
- `[files]` — Optional `originals_root` for independently persistent attachment/file originals, including documents and archives. `MEMOH_ORIGINALS_ROOT` overrides it. Empty preserves legacy workspace-first storage. `max_upload_bytes` sets the per-file HTTP attachment limit (bytes; `0` or omitted means 200 MiB). For example, `268435456` allows 256 MiB. Restart Server after changing it. Web fetches the same limit before streaming the file, and Server rejects an oversized stream with HTTP 413. The legacy inline WebSocket/channel limits remain unchanged. See [file originals](image-deployment.md#file-originals-outside-workspaces).
- `[containerd]` / `[docker]` / `[apple]` — Backend-specific runtime configuration
- `[postgres]` — PostgreSQL connection
- `[qdrant]` — Qdrant vector database connection
- `[sparse]` — Sparse (BM25) search service connection
- `[channel]` — Channel service HTTP/RPC listen addresses (split mode)
- `[internal_rpc]` — Server↔Channel internal RPC targets and shared secret; empty secret selects the embedded all-in-one mode
- `[web]` — Web frontend address
- `[registry]` — Provider registry (`providers_dir` pointing to `conf/providers/`)
- `[supermarket]` — Supermarket integration (base_url)

Provider YAML templates in `conf/providers/` define preset configurations for various LLM providers (OpenAI, Anthropic, GitHub Copilot, etc.).

Non-empty `files.originals_root` follows the existing `Config.resolvePaths`
convention: expand `~` / `~/...`, then resolve a relative path against the Server
process working directory at configuration load time, **not** the TOML file's
directory. Consumers receive that absolute path. Absolute paths keep their
location; an empty setting keeps legacy storage behavior. Changing the launch
working directory can therefore change a relative root on the next restart.
Use a stable working directory or an explicit deployment path. See
[data path contracts](data-path-resolution.md) for the separate Windows Pi layout.

Configuration templates available in `conf/`:
- `app.example.toml` — Default template
- `app.docker.toml` — Docker deployment
- `app.apple.toml` — macOS (Apple Virtualization backend)
- `app.windows.toml` — Windows

Development configuration in `devenv/`:
- `app.dev.toml` — Development (connects to devenv docker-compose)
