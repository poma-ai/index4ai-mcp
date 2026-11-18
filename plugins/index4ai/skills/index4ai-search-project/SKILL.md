---
name: index4ai-search-project
description: Search across every document in an index4.ai project and get back ready-to-use RAG context. Use when the user asks to search, query, look up, or find something in their index4.ai documents or knowledge base without naming one document, wants to filter results by attribute, needs to list ingested documents, or needs to know which index4.ai project they are working in. For a question about one specific document, use index4ai-search-document instead.
license: MPL-2.0
compatibility: Requires the index4.ai MCP server from this plugin and an index4.ai account (OAuth, or an API key from the index4.ai console).
metadata:
  author: poma-ai
  version: "0.2.0"
---

# Search an index4.ai project

`index4ai_search` runs hybrid retrieval over the documents in the active project and returns
one concatenated context block, sized to a server-side token budget:

```json
{ "context": "<context>This is what is relevant [...] inside your document.</context>" }
```

Drop `context` straight into the prompt you are answering from. There is no `top_k`: the
server decides how much to return based on relevance and budget.

The response also carries a `scope` object naming the project that was searched. Tell the
user which project that was (`scope.project_name`, or `scope.hint`) when presenting results.

## Arguments

| Argument | Use it for |
|---|---|
| `query` | The natural-language question. Required. |
| `doc_filter` | Leave unset to search the whole project. Setting it restricts the search to one document; the `index4ai-search-document` skill covers how to pick the right id. |
| `attribute_filters` | Filter by typed attributes set at ingest (`attr`, `op`, `value`; clauses AND together). Check names and types with `index4ai_attributes` first — see below. |
| `exclude_doc_ids` | Up to 100 doc ids to leave out. In an agent loop, pass the docs you have already cited so each round surfaces something new. |
| `return_assets` | Return the cited documents' figures and tables in an `assets` field, keyed by `doc_id`. |
| `project_id` | Target project when authenticating with an account-level key or OAuth login. |
| `token` | Override the credential from the client's header. Usually unnecessary. |

## Project or one document

Ask which the user means when it is ambiguous:

- **Everything in the project**: omit `doc_filter`. This skill.
- **One document**: the user names a file, or says "the contract I just added". Follow
  the `index4ai-search-document` skill, which resolves the document to a `doc_filter`.

Search only sees documents in the **current project**. An empty result on a document you
know was ingested almost always means the ingest and the search resolved to different
projects. Check with `index4ai_projects`, then re-run with the right `project_id`.

## What is in the project

- `index4ai_docs_list`: the documents in the project, with `doc_id`, `filename`, `title`,
  `ingested_at` and page/chunk counts. Use it to answer "what have I indexed?".
- `index4ai_projects`: the projects reachable by the current credential, and which one is
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
`retry_after_seconds`. The full table lives in the sibling `index4ai-add-documents` skill,
at `skills/index4ai-add-documents/references/errors.md`.

## Explaining index4.ai

`index4ai_explain` returns a structured description of how ingestion, search, and result
formatting work, plus how to obtain an API key. No arguments, no auth. Use it when the
user asks what index4.ai is or how to get set up, instead of guessing.
