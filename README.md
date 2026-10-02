# index4ai MCP

A hosted [Model Context Protocol](https://modelcontextprotocol.io) server for the [index4ai](https://index4.ai) context engine. Ingest documents and run semantic search from any MCP client (Claude Code, Claude Desktop, Cursor, or your own agent).

The server is meant to run at **`https://mcp.index4.ai`** (dev: `https://mcp-dev.index4.ai`). The hosts serve once the `index4ai-mcp` ArgoCD apps are enabled in poma-deploy (`clusters/gke-poma-nl/argocd/kustomization.yaml`). It talks to the index4ai API at `https://api.index4.ai/index4ai/v1` (spec: `poma-services-go/docs/api/index4ai/v1/openapi.yaml`). There is no local or stdio mode.

## Connect your agent

Auth is OAuth 2.0. On first connect the client gets a `401` with a `WWW-Authenticate` challenge and walks you through a browser login. MCP SDKs handle Dynamic Client Registration, authorize, token exchange and refresh automatically.

**Claude Code / Claude Desktop**
```json
{
  "mcpServers": {
    "index4ai": {
      "type": "http",
      "url": "https://mcp.index4.ai"
    }
  }
}
```

**Cursor** (`~/.cursor/mcp.json`)
```json
{
  "mcpServers": {
    "index4ai": {
      "url": "https://mcp.index4.ai"
    }
  }
}
```

To use an API key instead of OAuth, add `"headers": { "x-api-key": "your-api-key" }` (or `"Authorization": "Bearer your-api-key"`). Keys are created in the index4ai console (`console.index4.ai`, dev `console-dev.index4.ai`; the prod console is not live yet).

## Agent plugin

`plugins/index4ai/` packages the MCP declaration with two skills (`index4ai-ingest`, `index4ai-search`). Claude Code: `claude plugin marketplace add poma-ai/index4ai-mcp && claude plugin install index4ai@index4ai`. Other clients: `npx plugins add https://github.com/poma-ai/index4ai-mcp`, or see `plugins/index4ai/MANUAL.md`.

## Typical workflow

1. `index4ai_ingest_sync` (or `index4ai_ingest`) with `file_base64` for small files or `url` for anything large. Note the returned `job_id`. It is the `doc_id` once done, unless the result carries a `grill` object; then `grill.doc_id` is the document (see [Dedup and re-ingest](#dedup-and-re-ingest)).
2. `index4ai_search` with a question. Pass the `doc_id` as `doc_filter` to restrict to one document.

## Tools

| Tool | What it does |
|------|-------------|
| `index4ai_explain` | Explanation of how index4ai works: ingest flow, search, result format, API key setup. No arguments or auth. |
| `index4ai_ingest` | Starts ingest and returns `job_id` after upload. Does not wait for indexing. |
| `index4ai_ingest_sync` | Ingest and wait until the job reaches a terminal state. Returns `job_id` and status `events`. |
| `index4ai_ingest_resume` | Reconnect to an in-progress job's status stream and wait until terminal. |
| `index4ai_jobs_status` | Status snapshots for up to 50 jobs in one call. No streaming. |
| `index4ai_search` | Hybrid search returning concatenated context text for RAG. `doc_filter` restricts to one document. |
| `index4ai_docs_list` | All documents in the project (pages through the API internally). Metadata only. |
| `index4ai_attributes` | Typed attribute names declared in the project, their types and `max_names`. Call before passing `attributes` to an ingest tool and before building `attribute_filters`. |
| `index4ai_projects` | Accessible projects with their `project_id`. With a project API key, just the bound project. |

Every tool accepts an optional `token` (overrides the header credential). All except `index4ai_explain`, `index4ai_ingest_resume`, `index4ai_jobs_status` and `index4ai_projects` accept an optional `project_id`, needed only with an account-level key.

### `index4ai_ingest` / `index4ai_ingest_sync` arguments

Provide exactly one of `file_base64` or `url`.

| Argument | Type | Description |
|----------|------|-------------|
| `file_base64` | string | Standard base64 of the file bytes. Bounded by the MCP request body limit (~12 MiB of file by default). |
| `url` | string | Remote URL the index4ai API fetches and ingests. The MCP does not download it. Use for large files. |
| `filename` | string | Original basename (e.g. `report.pdf`). Inferred from content when omitted. |
| `attributes` | object | Typed document attributes, `{name: value}`. Values are strings, numbers, booleans, or non-empty arrays of one kind. Names match `^[a-z0-9_]{1,64}$`, are permanent per project and count against `max_names`. Sent as `X-Attributes`, capped at 64 names and 2048 characters. |
| `attribute_schema` | object | Type declarations `{name: {"type": "<type>"}}` for names in `attributes`, only where the value cannot say the type (`encrypted_text` always, `datetime` for a new name, the array type of an empty array). |
| `labels` | object | Legacy, being retired in favour of `attributes`. Sent as the `labels` attribute: each `{key: value}` pair becomes a `"key:value"` string in `attributes.labels` (sorted by key, after any `labels` you pass in `attributes`, duplicates dropped); no `X-Labels` header. index4ai types `labels` as `[]encrypted_text` (matched by token) unless the project already owns `labels` with another type, which then governs (an incompatible one fails at index4ai). Labels share the attribute limits: 64 elements, 64 names, 2048 characters of `X-Attributes`. Avoid `:` in keys. |

### `index4ai_search` arguments

| Argument | Type | Description |
|----------|------|-------------|
| `query` | string | Natural-language query (required). |
| `doc_filter` | string | `doc_id` to restrict the search to one document (routes to `/searchInDoc`). |
| `exclude_doc_ids` | array of string | Doc ids to exclude (max 100). |
| `attribute_filters` | array | `{attr, op, value}` clauses, ANDed. A filter on a name the project never used matches nothing. |
| `return_assets` | boolean | Return cited documents' figures/tables in `assets`, keyed by `doc_id`. |

Result count is bounded server-side by relevance and a token budget. There is no `top_k`.

### Dedup and re-ingest

When the API reports the outcome, `index4ai_ingest_sync`, `index4ai_ingest_resume` and each `index4ai_jobs_status` result carry a `grill` object (the API's field name): `{"deduplicated": bool, "doc_id": string, "replaced_doc_ids": [string]}`.

- New content: `deduplicated: false`, no `replaced_doc_ids`. `doc_id` = `job_id`.
- Unchanged file, same conversion build: `deduplicated: true`. Nothing new was stored; `doc_id` names the existing document. Use that id as `doc_filter`.
- Unchanged file, newer conversion build: `deduplicated: false`, and `replaced_doc_ids` lists the documents this job replaces.

### Errors

On error the MCP result has `isError: true`, a prose `error`, and a stable `code` (`missing_token`, `invalid_input`, `auth_expired`, `payment_required`, `project_protected`, `forbidden`, `too_many_jobs`, `upstream_error`, `transport_error`, `parse_error`, `job_failed`, `stream_error`). Retry only when `retryable` is true, after `retry_after_seconds`.

## Large uploads: `POST /ingest-upload`

Uploads a file without going through an MCP message. Same auth as MCP (`x-api-key` or `Authorization: Bearer`). Optional `X-Project-ID` is forwarded.

- Raw body (`application/octet-stream` or any non-multipart type), basename via `?filename=report.pdf` or `X-Filename`.
- Multipart (`multipart/form-data`) with a part named `file`.
- Metadata (optional): `X-Attributes` (JSON object) and `X-Attribute-Schema`, validated like the tools' `attributes` / `attribute_schema` before the body is read. A legacy `X-Labels` header (`key:value` items, comma-joined) is translated into the `labels` attribute and merged after any `labels` in `X-Attributes`; only `X-Attributes` is sent upstream, never `X-Labels`. Invalid metadata is a `400` `invalid_input`.

Response `201` with `{"job_id":"…"}`.

```bash
curl -sS -X POST "https://mcp.index4.ai/ingest-upload?filename=report.pdf" -H "x-api-key: $INDEX4AI_API_KEY" -H "Content-Type: application/octet-stream" --data-binary @./report.pdf
```

## Deployment

The image is built and pushed to `ghcr.io/poma-ai/index4ai-mcp:<short-sha>` on every push to `main` (`.github/workflows/docker-publish.yml`), which then sets that tag on the `index4ai-mcp` app in `poma-ai/poma-deploy` (dev). The step expects the manifest to name `ghcr.io/poma-ai/index4ai-mcp`; it exits non-zero without writing when it finds no such image, or when the tag is already current. The container listens on `:8080`: MCP at `/`, health at `GET /health`, protected-resource metadata at `GET /.well-known/oauth-protected-resource`.

| Variable | Default | Description |
|----------|---------|-------------|
| `POMA_API_BASE_URL` | `https://api.index4.ai` | index4ai API host. Only the origin is used; calls go to `<origin>/index4ai/v1`. Dev: `https://api-dev.index4.ai`. |
| `POMA_STATUS_API_BASE_URL` | API host + `/status/v1` | Job-status SSE service. |
| `POMA_AUTH_SERVER_URL` | derived | OAuth authorization server advertised in the protected-resource metadata. Must be the issuer the api reports, which is the poma-ai.com host of the same environment even on the index4ai hosts. Unset, it is derived from `POMA_API_BASE_URL` (`api-dev.index4.ai` → `https://api-dev.poma-ai.com`), falling back to `https://api.poma-ai.com`. |
| `POMA_API_JWT_SECRET` | unset | Enables local verification of OAuth bearer JWTs (HMAC-SHA256, `aud`, `exp`, `typ`). Must match the api. Requires `POMA_MCP_RESOURCE`. |
| `POMA_MCP_RESOURCE` | unset | The MCP's public URI, checked against the token `aud`. |
| `POMA_MCP_PUBLIC_URL` | `http://localhost:<port>` | Public base URL used in the metadata and the `WWW-Authenticate` challenge. Required behind the gateway: forwarded headers are not trusted. |
| `POMA_CONSOLE_URL` | `https://console.index4.ai` | Console linked from error messages. Dev: `https://console-dev.index4.ai`. |
| `GRILL_INGEST_MAX_BYTES` | 512 MiB | Max ingest payload; `0` = unlimited. `/ingest-upload` buffers the whole body in memory, so keep this well below the pod memory limit. |
| `GRILL_MCP_MAX_BODY_BYTES` | 16 MiB | Max MCP request body; a smaller `GRILL_INGEST_MAX_BYTES` lowers it. `0` = unlimited. Exceeding it returns a plain-text `413`. |
| `GRILL_TRUSTED_ORIGINS` | unset | Comma-separated origins allowed to make cross-origin state-changing requests. Only browser pages calling the server directly need this; non-browser MCP clients send neither `Sec-Fetch-Site` nor `Origin` and are always allowed. A sibling subdomain counts as cross-origin. |

There is no server-side API key: a request without a token in the header or the `token` argument fails with `missing_token`.

## Development

```bash
go build ./... && go vet ./... && go test ./...
```

## License

MPL-2.0
