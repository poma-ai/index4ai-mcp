# CLAUDE.md

`index4ai-mcp` is a hosted [Model Context Protocol](https://modelcontextprotocol.io) server wrapping the index4ai API (the retrieval product formerly called POMA Grill, now served on its own host and paths). It runs only as a streamable-HTTP server behind the gateway at `mcp.index4.ai` / `mcp-dev.index4.ai`. There is no stdio or local mode. Tools: `index4ai_ingest` (+ `_sync`, `_resume`), `index4ai_search`, `index4ai_jobs_status`, `index4ai_docs_list`, `index4ai_attributes`, `index4ai_projects`, `index4ai_explain`.

Forked from `poma-ai/poma-grill-mcp` (Go variant). `plugins/index4ai/` is the agent plugin (skills + MCP declaration pointing at `mcp.index4.ai`), listed by `.claude-plugin/marketplace.json`; its skills document the tool surface, so update them when tools change. Go identifiers still say `Grill` (`GrillError`, `grillClient`, `grill*.go`); user-facing names and messages say index4ai.

## Commands

```bash
go build ./... && go vet ./... && go test ./...
go run . -http 127.0.0.1:8080
```

Tests are offline (`httptest` stubs). They point `POMA_API_BASE_URL` at the stub, so stub paths are `/index4ai/v1/...`.

## API surface

The contract is `poma-services-go/docs/api/index4ai/v1/openapi.yaml` (generated from v3: `/grill/X` becomes `/X`, primeCut dropped). All calls go to `apiBaseURL()` = `https://api.index4.ai/index4ai/v1` (`tools/common.go`), wrappers in `tools/grill_client.go`:

| Call | Path |
|------|------|
| ingest (file or `X-Remote-URL`) | `POST /ingest` |
| search / search in doc | `POST /search`, `POST /searchInDoc` |
| list docs | `GET /docs?limit=100&cursor=` |
| attributes | `GET /attributes` |
| projects | `GET /projects?product=grill`, `GET /projects/info` (project keys) |
| job status poll | `GET /jobs/{job_id}/status` |

Two footguns:
- The status SSE stream is not under `/index4ai/v1`. It is `https://api.index4.ai/status/v1/jobs/{job_id}`, derived from the API origin (`statusAPIBaseURL`).
- OAuth is not on the index4ai host. `api(-dev).index4.ai/.well-known/oauth-authorization-server` answers with issuer `https://api(-dev).poma-ai.com`, so the protected-resource metadata advertises that host (`POMA_AUTH_SERVER_URL`, else derived from `POMA_API_BASE_URL` by mapping `.index4.ai` to `.poma-ai.com`), never the API host. A fixed prod default would put dev users on the prod login.

The API still names the job-status dedup object `grill` (`grill.doc_id` etc.), so tool output and descriptions keep that key.

## Auth

Per-call `token` argument, else the `x-api-key` / `Authorization: Bearer` header (`tools/common.go:getToken`). There is deliberately no server-side `POMA_API_KEY` fallback: the server is multi-tenant. When `POMA_API_JWT_SECRET` is set, `oauth.RequireBearer` verifies OAuth JWTs on `/`; non-JWT bearer tokens pass through as API keys.

## Deploy

Push to `main` builds `ghcr.io/poma-ai/index4ai-mcp:<short-sha>` and sets that tag on `base/apps/index4ai-mcp/dev` in `poma-ai/poma-deploy`. The manifest must name this image and run `/app/index4ai-mcp`; prod is promoted by hand. Env vars are listed in `README.md`.

## Domain

User-facing links point to `https://console.index4.ai` (dev `console-dev.index4.ai`) through `consoleURL()`; never hardcode it. Company domain is `poma-ai.com`, never `poma.ai`; the console is never `app.poma-ai.com`.
