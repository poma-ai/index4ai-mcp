---
name: index4ai-search
description: Search documents already ingested into the index4ai context engine and get back ready-to-use RAG context. Use when the user asks to search, query, look up, find, or ask questions about their index4ai documents, wants to restrict a search to one document, needs to list ingested documents, or needs to know which index4ai project they are working in.
license: MPL-2.0
compatibility: Requires the index4ai MCP server from this plugin and an index4ai account (OAuth, or an API key from the index4ai console).
metadata:
  author: poma-ai
  version: "0.1.0"
---

# Search index4ai

`index4ai_search` runs hybrid retrieval over the documents in the active project and returns
one concatenated context block, sized to a server-side token budget:

```json
{ "context": "<context>This is what is relevant [...] inside your document.</context>" }
```

Drop `context` straight into the prompt you are answering from. There is no `top_k` — the
server decides how much to return based on relevance and budget.

## Arguments

| Argument | Use it for |
|---|---|
| `query` | The natural-language question. Required. |
| `doc_filter` | Restrict to a single document. The value is the `job_id` returned by ingest, which is normally the same string as `doc_id` — but when the ingest result or `index4ai_jobs_status` carried a `grill` object, use `grill.doc_id` instead: on a dedup hit the two differ and the `job_id` matches no indexed document. |
| `attribute_filters` | Filter by typed attributes set at ingest (`attr`, `op`, `value`; clauses AND together). Check names and types with `index4ai_attributes` first — see below. |
| `exclude_doc_ids` | Up to 100 doc ids to leave out. In an agent loop, pass the docs you have already cited so each round surfaces something new. |
| `return_assets` | Return the cited documents' figures and tables in an `assets` field, keyed by `doc_id`. |
| `project_id` | Target project when authenticating with an account-level key or OAuth login. |
| `token` | Override the credential from the client's header. Usually unnecessary. |

## Scope one search or many

Ask which the user means when it is ambiguous:

- **One document** — pass `doc_filter`. Use this right after an ingest, when the user says
  "the contract I just ingested", or when they name a specific file.
- **Everything in the project** — omit `doc_filter`.

Search only sees documents in the **current project**. An empty result on a document you
know was ingested almost always means the ingest and the search resolved to different
projects. Check with `index4ai_projects`, then re-run with the right `project_id`.

## Finding the right document

- `index4ai_docs_list` — the documents in the project, with their ids and labels. Use it to
  turn "the Q1 report" into a `doc_filter` without asking the user for an id.
- `index4ai_projects` — the projects reachable by the current credential, and which one is
  the default.

## Filtering by attribute

Attributes are set at ingest through the `attributes` argument of the ingest tools. Before
building `attribute_filters`, call **`index4ai_attributes`** to learn which attribute names the
project actually has and the type of each. Use the exact name it returns: a
filter on a name the project has never used matches **nothing** rather than being ignored,
so a guessed `doc_year` where the project stores `year` silently empties the result. The
type decides which operators work (`encrypted_text`, for instance, matches exact tokens
only — no stemming, no partial words); an operator the type does not offer returns an
error naming the ones it does.

## Query phrasing

Retrieval is semantic, so full questions beat keyword soup: "what are the termination
notice periods" pulls better context than "termination". When the user's question is
broad, run one search and read the returned context before deciding whether a second,
narrower query is worth it — each call spends its own token budget.

## Errors

Errors carry a machine-readable `code`. `auth_expired`, `forbidden`, and `invalid_input`
are terminal; `transport_error` and 5xx `upstream_error` are retryable after
`retry_after_seconds`. The full table lives in the sibling `index4ai-ingest` skill, at
`skills/index4ai-ingest/references/errors.md`.

## Explaining index4ai

`index4ai_explain` returns a structured description of how ingestion, search, and result
formatting work, plus how to obtain an API key. No arguments, no auth. Use it when the
user asks what index4ai is or how to get set up, instead of guessing.
