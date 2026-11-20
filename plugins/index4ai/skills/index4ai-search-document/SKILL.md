---
name: index4ai-search-document
description: Search inside one specific document in the index4.ai context engine and get back ready-to-use RAG context from that document only. Use when the user asks a question about a named file ("what does the Q1 report say about churn"), about the document they just added or uploaded, or asks to search in, look inside, summarise a section of, or find a clause in one index4.ai document. For questions across all documents, use index4ai-search-project instead.
license: MPL-2.0
compatibility: Requires the index4.ai MCP server from this plugin and an index4.ai account (OAuth, or an API key from the index4.ai console).
metadata:
  author: poma-ai
  version: "0.1.0"
---

# Search inside one index4.ai document

There is no separate search-in-document tool. Call `index4ai_search` with `doc_filter` set
to the document's `doc_id`; the server then searches that document only and returns one
concatenated context block, sized to a server-side token budget:

```json
{ "context": "<context>This is what is relevant [...] inside your document.</context>" }
```

Drop `context` straight into the prompt you are answering from. There is no `top_k`.

The whole job of this skill is getting `doc_filter` right. A wrong id matches no indexed
document, so the search comes back with empty context or an `upstream_error` and never
with a hint about which id would have worked.

## Step 1: resolve the document to a `doc_id`

Pick the case that matches how the user referred to the document.

| The user means | Where the `doc_id` comes from |
|---|---|
| The document just ingested in this conversation | The ingest result: `grill.doc_id` when a `grill` object is present, otherwise `job_id` |
| A job that was started earlier and may still be running | `index4ai_jobs_status` (or `index4ai_ingest_resume` to wait), then the same rule |
| A finished job where only the `job_id` is known | `index4ai_docs_list`, find the entry whose `source_job_id` is that `job_id`, take its `doc_id` |
| A document named by filename or title ("the Q1 report") | `index4ai_docs_list`, match on `filename` or `title`, take its `doc_id` |
| A `doc_id` the user pasted | Use it as given |

### After an ingest: prefer `grill.doc_id`

`index4ai_ingest_sync`, `index4ai_ingest_resume` and each `index4ai_jobs_status` result can
carry a `grill` object (the API's field name):
`{"deduplicated": bool, "doc_id": string, "replaced_doc_ids": [string]}`.

- **`grill` present**: pass `grill.doc_id`. On a dedup hit (`deduplicated: true`) the bytes
  were already indexed, `grill.doc_id` names the existing document, and the new `job_id`
  matches nothing.
- **`grill` absent and the job is terminal**: pass `job_id`; the two are the same string.
- **Job not terminal yet**: the document is not searchable. Wait with
  `index4ai_ingest_resume` or poll `index4ai_jobs_status` before searching.

### By name: `index4ai_docs_list`

`index4ai_docs_list` returns every document in the project with `doc_id`, `filename`,
`title`, `ingested_at`, `source_job_id` and page/chunk counts. Use it to turn "the Q1
report" into a `doc_id` without asking the user for an id.

- **One clear match**: use it, and say which file you searched.
- **Several plausible matches** (two versions of the same report, say): list them with
  `filename` and `ingested_at` and ask the user which one. Do not guess.
- **No match**: the document may live in another project. Check `index4ai_projects`, then
  list again with the right `project_id`. If it is nowhere, offer to add it with the
  `index4ai-add-documents` skill.

If the response has a `note` field, fewer documents were returned than exist; tell the
user, since the one they want may be among the missing.

## Step 2: search

| Argument | Use it for |
|---|---|
| `query` | The natural-language question. Required. |
| `doc_filter` | The `doc_id` from step 1. Required for this skill. |
| `return_assets` | Return the document's cited figures and tables in an `assets` field, keyed by `doc_id`. |
| `attribute_filters` | Rarely needed for one document. See the `index4ai-search-project` skill. |
| `project_id` | The project the document lives in, when authenticating with an account-level key or OAuth login. |
| `token` | Override the credential from the client's header. Usually unnecessary. |

Retrieval is semantic, so full questions beat keyword soup: "what are the termination
notice periods" pulls better context than "termination". For a broad request such as
"summarise this document", run a few focused questions (purpose, parties, key figures,
conclusions) instead of one vague query; each call spends its own token budget.

The response carries a `scope` object naming the project that was searched. Tell the user
the project (`scope.project_name`, or `scope.hint`) along with the document you searched.

## When the result is empty or the search errors

Work through these in order before telling the user the document says nothing on the topic:

1. **Wrong id.** A `job_id` was used where a `grill.doc_id` existed. Re-read the ingest or
   status result, or look the document up with `index4ai_docs_list` (by `source_job_id`,
   `filename` or `title`).
2. **Wrong project.** The ingest and the search resolved to different projects. The search
   result's `scope` names the project searched; `index4ai_docs_list` shows whether the
   document is in it. If not, check `index4ai_projects` and pass the right `project_id`.
3. **Not indexed yet.** The job has not reached a terminal state. Check
   `index4ai_jobs_status`.
4. **The document really has nothing relevant.** Rephrase the question once, then say so.

## Worked example

User: "what does the Acme MSA say about termination?"

1. `index4ai_docs_list`. One document has `filename: "acme-msa-2025.pdf"`; take its `doc_id`.
2. `index4ai_search` with `query: "what are the termination rights and notice periods"` and
   `doc_filter` set to that `doc_id`.
3. Answer from `context`, naming the file and the project searched.

## Errors

Errors carry a machine-readable `code`. `auth_expired`, `forbidden`, and `invalid_input`
are terminal; `transport_error` and 5xx `upstream_error` are retryable after
`retry_after_seconds`. The full table lives in the sibling `index4ai-add-documents` skill,
at `skills/index4ai-add-documents/references/errors.md`.
