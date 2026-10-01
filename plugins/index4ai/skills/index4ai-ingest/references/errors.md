# index4ai error codes

Every index4ai tool returns an error envelope alongside its normal output:

```json
{
  "error": "human-readable message",
  "code": "too_many_jobs",
  "retryable": true,
  "retry_after_seconds": 30
}
```

`isError` is `true` on the MCP response. Branch on `code`, never on the text of `error`.

## Retryable

Retry the identical call after `retry_after_seconds` (default to ~30s if absent).

| Code | Meaning | What to do |
|---|---|---|
| `too_many_jobs` | Account is at its concurrent-job ceiling. The document was **not** ingested. | Wait, retry the same call, and hold off on new ingests until running jobs finish. |
| `transport_error` | Network failure reaching the API. | Retry with backoff. |
| `stream_error` | The status (SSE) stream dropped mid-job. | The job may still be running — prefer `index4ai_ingest_resume` with the known `job_id` over re-uploading. |
| `upstream_error` | 5xx from the index4ai API. | Retry with backoff. A 4xx `upstream_error` is terminal. |

## Terminal

Do not retry. Fix the cause or abort and tell the user.

| Code | Meaning | What to do |
|---|---|---|
| `missing_token` | No credential on the request. | Authorize the server (OAuth via the client's MCP settings), set an `x-api-key` / `Authorization: Bearer` header in the MCP client config, or pass `token` on the call. |
| `auth_expired` | Credential rejected or expired. | Re-authenticate, or generate a new key in the index4ai console (the URL is in the error message). Say explicitly which key was rejected and which one is needed. |
| `payment_required` | Plan limit hit. | Upgrade in the index4ai console (the URL is in the error message). |
| `forbidden` | Credential is valid but not authorized for this project or action. | Check the project with `index4ai_projects`; the key may be scoped to a different one. |
| `project_protected` | The target project blocks this operation. | Choose another project or lift the protection in the console. |
| `invalid_input` | Bad arguments — e.g. zero or both of `file_base64`/`url`, a payload over `GRILL_INGEST_MAX_BYTES`, or a `project_id` that differs from the project the project API key is bound to (API `409 project_id_conflict`). | Fix the arguments. Never retry unchanged. |
| `parse_error` | The API response could not be decoded. | Report it; likely a server-side or version-skew bug. |
| `job_failed` | Ingestion ran and failed — usually an unsupported or corrupt file. | Report the message to the user. Re-uploading the same bytes will fail the same way. |

An MCP request body over the server's limit (16 MiB by default, about 12 MiB of file as
base64) is refused by the transport with a plain-text HTTP `413`, not this envelope. Use
`url` or `POST /ingest-upload` for files that size.
