---
name: index4ai-ingest
description: Ingest documents (PDF, DOCX, PPTX, XLSX, HTML, Markdown, plain text, images) into the index4ai context engine so they become semantically searchable. Use when the user says "index my <file>", or asks to ingest, index, upload, or add a document to index4ai, or wants a PDF/report/contract made searchable. Also covers job status polling and resuming an in-progress ingest.
license: MPL-2.0
compatibility: Requires the index4ai MCP server from this plugin and an index4ai account (OAuth, or an API key from the index4ai console).
metadata:
  author: poma-ai
  version: "0.1.0"
---

# Ingest into index4ai

index4ai turns a document into semantically chunked, searchable context. Ingest returns a
`job_id`; once the job reaches a terminal state that same id is the `doc_id` you pass to
`index4ai_search` as `doc_filter` — except when the result carries a `grill` object, where
`grill.doc_id` is the document to filter on. See [Re-ingesting a file](#re-ingesting-a-file).

## Pick the right tool

| Situation | Tool |
|---|---|
| One file, you want the result before continuing | `index4ai_ingest_sync` |
| One file, fire-and-forget (poll later) | `index4ai_ingest` |
| Several files (up to 50) | `index4ai_ingest_batch` with one item per file, then `index4ai_jobs_status` |
| You already have a `job_id` and just need to wait | `index4ai_ingest_resume` |
| Checking up to 50 jobs at once | `index4ai_jobs_status` |

Default to `index4ai_ingest_sync` for a single file. It waits and returns the status events,
so you can tell the user the document is actually searchable rather than merely queued.

## Choosing the input

The server is hosted, so it cannot read paths on the user's machine. Provide **exactly
one** of `file_base64` or `url`.

- **`url`** — a remote document the index4ai API fetches itself. The MCP server does not
  download it. Prefer this for anything large or already online.
- **`file_base64`** — the file bytes, standard base64. Small files only: the MCP request
  body is capped (about 12 MiB of file by default), and base64 inflates memory on both ends.
  For a large local file, see [Very large files](#very-large-files).

Optional on both: `filename` (basename shown in the UI; inferred from content when omitted),
typed `attributes` / `attribute_schema` (next section), and `labels`.

`labels` is **legacy and being retired in favour of attributes** — prefer `attributes` for
new work. It still works: a flat `{key: value}` map, sent as the `labels` attribute
(`"key:value"` strings in `attributes.labels`, after any you pass there; no `X-Labels`
header). index4ai types it `[]encrypted_text`, matched by token, unless the project already
owns `labels` with another type, which then governs. Labels count against the attribute
limits (64 elements, 64 names, 2048 characters). Avoid `:` in label keys.

## Typed attributes: call `index4ai_attributes` first

`index4ai_ingest` and `index4ai_ingest_sync` take typed document attributes, which
`index4ai_search` can later filter on with `attribute_filters`:

- `attributes` — `{name: value}`, where a value is a string, number, boolean, or an array
  of those. `null` is refused. E.g. `{"region": "emea", "year": 2024}`.
- `attribute_schema` — `{name: {"type": "<type>"}}`, only for types the value cannot
  express, and only for names present in `attributes`.

Steps:

1. **Call `index4ai_attributes` first.** It returns every name the project has declared,
   each with its type, plus `max_names`, the project's cap on distinct names.
2. **Reuse an existing name and type where one fits.** If `year` exists, do not invent
   `doc_year`: the second name splits the data, so a filter on either misses half the
   documents. A name, once declared, is **permanent** for the project and counts against
   `max_names`; it cannot be renamed or freed. New names must match `^[a-z0-9_]{1,64}$`.
3. **Pass the values in `attributes`.** Strings, numbers, booleans and their arrays are
   typed by value. An array must be non-empty and of one kind — all strings, all ints, all
   floats or all booleans (`[1, 2.5]` mixes int and float and is refused). A whole number
   such as `1.0` counts as an int; to keep a float type for whole numbers, declare `float`
   or `[]float`. An empty array is accepted only when `attribute_schema` declares its array
   type, e.g. `{"tags": {"type": "[]string"}}`.
4. **Declare a type in `attribute_schema` only when needed**, and only for a name you also
   pass in `attributes`. `encrypted_text` must **always** be declared — undeclared, a new
   name is stored as a plain string. Declare `datetime` when introducing a new datetime
   name; its values must be RFC3339 with a timezone (`"2024-05-01T12:00:00Z"`,
   `"...+02:00"`) or a plain `YYYY-MM-DD` date. Types are `string`, `int`, `float`, `bool`,
   `datetime`, `encrypted_text` and their `[]` array forms; anything else is refused. A
   declaration that conflicts with the type already in force for that name is rejected.

Each of the two headers is capped at 2048 characters of compact JSON (and 64 names).
Anything larger, or a malformed name, is refused with `invalid_input` before anything is
uploaded — nothing is truncated. If `index4ai_attributes` fails with a retryable
`upstream_error` (HTTP 503, schema unreadable), wait and retry; do not declare new names
blind.

## Always report the project

Ingest responses carry a `scope` object naming the project the document landed in. Tell
the user which project that was (`scope.project_name`, or `scope.hint`). Documents are
only searchable within their project, so a silent ingest into the wrong project is the
most common reason a later search comes back empty.

With an **account-level** API key or OAuth login, pass `project_id` to choose the target;
without it the account's default project is used. With a **project** key the scope is fixed
and `project_id` is unnecessary. Use `index4ai_projects` to list available projects and see
which one is the default.

## Backpressure

A response with `code: "too_many_jobs"` and `retryable: true` means the account is at its
concurrent-job ceiling and **the document was not ingested**. Wait
`retry_after_seconds`, then retry the identical call. Do not retry in a tight loop, and
pause any further ingests until capacity frees up. When ingesting several files, submit
them one at a time and check `index4ai_jobs_status` before sending more.

## Errors

Every error carries a machine-readable `code`. Branch on the code, never on the prose
message. Retry only when `retryable` is true, and only after `retry_after_seconds`.
Full table: [references/errors.md](references/errors.md).

## Re-ingesting a file

Re-ingesting is safe and the job succeeds either way, but what index4ai did with it depends
on whether anything changed. When the gateway reports the outcome, `index4ai_ingest_sync`,
`index4ai_ingest_resume` and each `index4ai_jobs_status` result carry a `grill` object (the
API's field name): `{"deduplicated": bool, "doc_id": string, "replaced_doc_ids": [string]}`.

Whenever that object is present, **`grill.doc_id` is the document to pass as `doc_filter`** —
not `job_id`. The two differ on a dedup hit, and filtering on the `job_id` there matches no
indexed document and silently returns nothing.

- `deduplicated: true` — the same bytes under the same conversion build were already indexed.
  Nothing new was stored (conversion credits are still consumed) and `doc_id` names the
  document that was already there.
- `deduplicated: false` with `replaced_doc_ids` — a newer conversion build replaced the
  listed older documents; `doc_id` is the replacement.
- `deduplicated: false` with no `replaced_doc_ids` — a plain ingest; `doc_id` equals `job_id`.

The object is absent before the job reaches the indexing stage.

## Worked example

User: "index https://example.com/contract.pdf and find the indemnification clause"

1. `index4ai_ingest_sync` with `url: "https://example.com/contract.pdf"`.
2. Read `job_id`, `grill` and `scope` from the result; tell the user the project name.
3. `index4ai_search` with `query: "indemnification clause"`, and `doc_filter` set to
   `grill.doc_id` when the result carried a `grill` object, otherwise to `job_id`.

## Very large files

A large local file the agent cannot hand over as a URL can bypass the MCP message entirely:

- `POST https://mcp.index4.ai/ingest-upload` with the raw bytes (`?filename=report.pdf`) or
  a multipart field `file`, authenticated with the same `x-api-key` / `Authorization:
  Bearer` header. It returns `{"job_id": "…"}`.
- Or `POST` the bytes to the index4ai API itself, `https://api.index4.ai/v1/ingest`,
  with `Content-Type: application/octet-stream` and
  `Content-Disposition: attachment; filename="…"`.

Either way you get a `job_id`, usable with `index4ai_ingest_resume`, `index4ai_jobs_status`
and as `doc_filter` under the same caveat as above.
