# index4.ai agent plugin

An [Agent Plugin](https://agent-plugins.org) (spec 1.0.0) that gives any supporting agent
the index4.ai context engine: ingest documents, then search them semantically.

## Contents

```
index4ai/
├── plugin.json                              # Agent Plugins manifest
├── mcp.json                                 # Agent Plugins MCP declaration
├── .claude-plugin/plugin.json               # Claude Code manifest
├── .mcp.json                                # Claude Code MCP declaration
├── MANUAL.md                                # install guide: Claude Code, Cursor, Codex
├── icon.png                                 # 512x512 listing icon (Anthropic directory)
└── skills/
    ├── index4ai-add-documents/
    │   ├── SKILL.md                         # ingest, batch, resume, job status
    │   └── references/errors.md             # error codes and retry rules
    ├── index4ai-search-project/
    │   └── SKILL.md                         # search across a project, attribute filters
    └── index4ai-search-document/
        └── SKILL.md                         # search inside one document, doc id resolution
```

`plugin.json` and `mcp.json` validate against the 1.0.0 schemas
([plugin](https://agent-plugins.org/schemas/1.0.0/plugin.schema.json),
[mcp](https://agent-plugins.org/schemas/1.0.0/mcp.schema.json)). The skills follow the
[Agent Skills](https://agentskills.io/specification) spec.

The `.claude-plugin/` and `.mcp.json` files are Claude Code's own format, which is not the
Agent Plugins standard: Claude Code reads `.mcp.json`, never `mcp.json`. Both MCP
declarations point at the same endpoint; keep them in sync.

## Install

In a Claude Code session (2.1.275 or later), one step adds the marketplace and installs:

```
/plugin install index4ai --marketplace poma-ai/index4ai-mcp
```

From a shell, `claude plugin install index4ai --marketplace poma-ai/index4ai-mcp` does the
same on 2.1.292 or later; on older versions run
`claude plugin marketplace add poma-ai/index4ai-mcp` and then
`claude plugin install index4ai@index4ai`. The marketplace is served by
`.claude-plugin/marketplace.json` at the repo root.

For Cursor, Codex and other clients, `npx plugins add https://github.com/poma-ai/index4ai-mcp`
installs into whichever tools it detects. Per-client steps are in [MANUAL.md](MANUAL.md).

## MCP server

`mcp.json` points at the hosted endpoint, so there is nothing to install:

```json
{
  "index4ai": {
    "type": "streamable-http",
    "url": "https://mcp.index4.ai"
  }
}
```

Auth is OAuth 2.0. The first connection returns `401` with a `WWW-Authenticate` challenge
and the client walks you through a browser login. MCP SDKs handle registration, token
exchange, and refresh automatically.

Prefer an API key over OAuth? Add headers:

```json
{
  "index4ai": {
    "type": "streamable-http",
    "url": "https://mcp.index4.ai",
    "headers": { "x-api-key": "your-api-key" }
  }
}
```

The server is hosted only, so it cannot read files on your machine. Ingest remote
documents by `url` and small ones by `file_base64`; large local files go through
`POST https://mcp.index4.ai/ingest-upload` (see the ingest skill).

## Tools the plugin exposes

| Tool | What it does |
|---|---|
| `index4ai_explain` | How index4.ai works, and how to get an API key. No auth. |
| `index4ai_ingest` | Start an ingest, return `job_id` immediately. |
| `index4ai_ingest_sync` | Ingest and wait for a terminal state. |
| `index4ai_ingest_batch` | Ingest up to 50 documents in one call; one `job_id` or error per item. |
| `index4ai_ingest_resume` | Reattach to a running job's status stream. |
| `index4ai_jobs_status` | Status snapshots for up to 50 jobs. |
| `index4ai_search` | Hybrid search returning concatenated RAG context. |
| `index4ai_docs_list` | Documents in the project, with ids and metadata. |
| `index4ai_attributes` | Typed attribute names and types already declared in the project; call before passing `attributes` to an ingest tool, and reuse a listed name where one fits. |
| `index4ai_projects` | Projects reachable by the credential, and the default. |

## Versioning

`plugin.json` tracks the tool surface it documents (currently `0.3.1`). When the tools in
`tools/` change, update the skills and bump `version` in `plugin.json` and in
`.claude-plugin/plugin.json`. The marketplace entry carries no version on purpose: Claude
Code takes it from `.claude-plugin/plugin.json`, and installed users only receive an update
when that value changes.

## License

MPL-2.0
