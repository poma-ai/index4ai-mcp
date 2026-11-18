# Install guide

This plugin ships two kinds of thing:

- **`skills/index4ai-add-documents`**, **`skills/index4ai-search-project`** and **`skills/index4ai-search-document`**: instructions the agent loads when a task matches.
- **One MCP server**: the hosted index4.ai endpoint at `https://mcp.index4.ai`, which provides the `index4ai_*` tools.

Skills are portable across clients. The MCP server has to be declared in each client's
own config format, so the same server appears three times below with different syntax.

## Quickest path: the `plugins` CLI

[`vercel-labs/plugins`](https://github.com/vercel-labs/plugins) installs an Agent Plugins
package into whichever agent tools it detects, translating the vendor-neutral format into
each one's native layout:

```bash
npx plugins add https://github.com/poma-ai/index4ai-mcp
```

Point it at the repository, not at this directory — the CLI shallow-clones the repo to
`~/.cache/plugins/` and scans it, finding `index4ai` under `plugins/`. Use the full
`https://github.com/...` URL; the `owner/repo` shorthand fails to resolve this repo.

It targets Claude Code, Cursor, Codex, Grok Build, Kimi Code, GitHub Copilot CLI, and VS
Code. Restrict it with `--target claude-code` and choose `--scope user|project|local`.
`npx plugins discover https://github.com/poma-ai/index4ai-mcp` previews what it found
without installing; `npx plugins targets` lists the tools it detected on your machine.

For Claude Code it registers a one-plugin marketplace and installs from it, so the plugin
shows up in `claude plugin list` and `/plugin`. Remove it with
`claude plugin marketplace remove index4ai`, which takes the installed plugin with it.

**One caveat, measured rather than assumed:** the CLI's Claude Code translation copies
`skills/` but does not generate a `.mcp.json`, so a plugin that declares its MCP server
only in the standard `mcp.json` installs with skills and no tools. This plugin ships its
own `.mcp.json`, so the server does come across — that file is the reason. Same for
`.claude-plugin/plugin.json`: without it the generated manifest carries an empty
description and version `0.0.0`.

The manual routes below need no extra tooling and are worth knowing when the CLI's
translation does not do what you want. They all reference a local clone:

```bash
git clone https://github.com/poma-ai/index4ai-mcp && export INDEX4AI_PLUGIN="$PWD/index4ai-mcp/plugins/index4ai"
```

`$INDEX4AI_PLUGIN` stands in for that path in every command below. Set it again in each new
shell, or substitute the path directly.

## Which file each client reads

The plugin carries two manifests on purpose — the portable one and Claude Code's.

| File | Purpose | Claude Code | Cursor | Codex |
|---|---|---|---|---|
| `plugin.json` | [Agent Plugins 1.0.0](https://agent-plugins.org) manifest | ignored | ignored | ignored |
| `mcp.json` | Agent Plugins MCP declaration | ignored | ignored | ignored |
| `.claude-plugin/plugin.json` | Claude Code manifest | **read** | ignored | ignored |
| `.mcp.json` | Claude Code MCP declaration | **read** | ignored | ignored |
| `skills/<name>/SKILL.md` | Agent Skills | **read** | copy into `.cursor/skills/` | copy into `~/.codex/skills/` |

Claude Code does not implement the Agent Plugins standard: it reads `.mcp.json`, not
`mcp.json`, and it wants `"type": "http"` where the standard says `"streamable-http"`.
Declaring an MCP server only in `mcp.json` loads nothing in Claude Code, silently — and
the `plugins` CLI does not bridge that gap either. Keep the two MCP files in sync when you
change the endpoint.

---

## Claude Code

Three routes: `npx plugins add` (above), a one-session flag, or a copy into your skills
directory. All three end up with the same three skills and one MCP server.

### Try it for one session

```bash
claude --plugin-dir "$INDEX4AI_PLUGIN"
```

The skills become `/index4ai:index4ai-add-documents`, `/index4ai:index4ai-search-project`
and `/index4ai:index4ai-search-document`; the server
registers as `plugin:index4ai:index4ai`. Run `/reload-plugins` after editing a file.

### Install it permanently

Copy the plugin into your skills directory. It auto-loads in every session, no
marketplace and no flag:

```bash
cp -R "$INDEX4AI_PLUGIN" ~/.claude/skills/index4ai
```

Restart Claude Code, then authorize the server with `/mcp` — the first connection returns
a `401` and walks you through a browser login.

Symlink instead of copying if you want edits in the repo to take effect immediately:

```bash
ln -s "$INDEX4AI_PLUGIN" ~/.claude/skills/index4ai
```

Remove it with `rm -rf ~/.claude/skills/index4ai`.

### Verify

```bash
claude plugin validate "$INDEX4AI_PLUGIN"
```

Then, in a session, `/help` lists the three skills under the `index4ai` namespace and
`/mcp` shows `index4ai` as connected.

### Distribute to a team

The repo root carries a `.claude-plugin/marketplace.json` naming the marketplace
`index4ai`, so teammates install straight from GitHub:

```bash
claude plugin marketplace add poma-ai/index4ai-mcp && claude plugin install index4ai@index4ai
```

On Claude Code 2.1.292 or later, `claude plugin install index4ai --marketplace poma-ai/index4ai-mcp`
does both in one command (`/plugin install index4ai --marketplace poma-ai/index4ai-mcp`
inside a session, 2.1.275 or later).

Upgrading from 0.2.x: the skills were renamed (`index4ai-ingest` is now
`index4ai-add-documents`, `index4ai-search` is now `index4ai-search-project`). A
marketplace install picks that up with `claude plugin update index4ai@index4ai`. Symlinks
made for Cursor or Codex under the old names dangle after a `git pull`: remove them and
rerun the link command for that client.

Add `--scope project` to the marketplace command to declare it for a repo rather than for
your user. Undo with `claude plugin marketplace remove index4ai`, which takes the installed
plugin with it.

`npx plugins add https://github.com/poma-ai/index4ai-mcp` registers its own one-off
marketplace instead, and needs no prior setup.

### API key instead of OAuth

Add headers to `.mcp.json`:

```json
{
  "mcpServers": {
    "index4ai": {
      "type": "http",
      "url": "https://mcp.index4.ai",
      "headers": { "x-api-key": "your-api-key" }
    }
  }
}
```

---

## Cursor

Cursor reads skills from `.cursor/skills/` (project) or `~/.cursor/skills/` (global), and
MCP servers from `.cursor/mcp.json` or `~/.cursor/mcp.json`. It does not read a plugin
directory, so point it at the skills and the server separately.

### Skills

```bash
mkdir -p ~/.cursor/skills && for s in index4ai-add-documents index4ai-search-project index4ai-search-document; do ln -s "$INDEX4AI_PLUGIN/skills/$s" ~/.cursor/skills/$s; done
```

Use `.cursor/skills/` inside a repo instead when the skills should only apply to that
project. Cursor also accepts `.agents/skills/` and `~/.agents/skills/`.

### MCP server

Add to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "index4ai": {
      "url": "https://mcp.index4.ai"
    }
  }
}
```

Cursor infers the transport from `url`, so there is no `type` field. For an API key
instead of OAuth:

```json
{
  "mcpServers": {
    "index4ai": {
      "url": "https://mcp.index4.ai",
      "headers": { "x-api-key": "your-api-key" }
    }
  }
}
```

Check Settings → MCP; the server should list the `index4ai_*` tools once authorized.

---

## Codex

Codex reads skills from `~/.codex/skills/` (personal) or `.agents/skills/` (project, also
searched at the repo root), and MCP servers from `~/.codex/config.toml`.

### Skills

```bash
mkdir -p ~/.codex/skills && for s in index4ai-add-documents index4ai-search-project index4ai-search-document; do ln -s "$INDEX4AI_PLUGIN/skills/$s" ~/.codex/skills/$s; done
```

Invoke one explicitly with `$index4ai-add-documents`, or let Codex match it against the skill's
`description`.

### MCP server

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.index4ai]
url = "https://mcp.index4.ai"
bearer_token_env_var = "INDEX4AI_API_KEY"
```

Then `export INDEX4AI_API_KEY=your-api-key`. Dropping `bearer_token_env_var` makes Codex use
its default OAuth mode instead.

Verify with `codex mcp list`.

---

## Troubleshooting

| Symptom | Cause |
|---|---|
| Skills load, no `index4ai_*` tools | The MCP server is registered but unauthorized. Authorize it (`/mcp` in Claude Code) or set an API key header. |
| Nothing loads in Claude Code | Wrong directory passed to `--plugin-dir`. It must be `plugins/index4ai`, the directory holding `skills/`. |
| Installed via `npx plugins add`, skills work, no tools | The plugin's `.mcp.json` is missing. The CLI does not generate one for Claude Code. |
| Server silently absent in Claude Code | `.mcp.json` is missing, or its server uses `"type": "streamable-http"`. Claude Code needs `"type": "http"`. |
| `401` on every call | The API key is rejected or expired. Generate a new one in the index4.ai console (`console.index4.ai`). |
| Search returns nothing for a document you ingested | Ingest and search resolved to different projects. Check with `index4ai_projects`, then pass `project_id`. |
