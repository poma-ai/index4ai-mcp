package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ingestBatchMaxItems          = 50
	ingestBatchDefaultConcurrent = 5
	ingestBatchMaxConcurrent     = 10
)

var ingestBatchInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"items": {
			Type:        "array",
			Description: fmt.Sprintf("Documents to ingest, max %d. Each item has exactly one of url (the server fetches it; preferred) or file_base64 (small files; the whole MCP request body is capped, so a batch of base64 files must fit in it together), plus an optional filename.", ingestBatchMaxItems),
			Items: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"url":         {Type: "string", Description: ingestURLDescription},
					"file_base64": {Type: "string", Description: "Standard base64 of the file bytes. Mutually exclusive with url."},
					"filename":    {Type: "string", Description: "Original basename (e.g. report.pdf). Optional; inferred from content when possible."},
				},
			},
		},
		"token": {
			Type:        "string",
			Description: "index4.ai API key or token. Usually not needed: the server takes the token from the Authorization (Bearer) or x-api-key header in the MCP client config. Only pass explicitly to override.",
		},
		"concurrency": {
			Type:        "integer",
			Description: fmt.Sprintf("Upload concurrency (default %d, max %d). Use 1 for free-tier accounts.", ingestBatchDefaultConcurrent, ingestBatchMaxConcurrent),
		},
		"attributes":       ingestAttributesSchema(),
		"attribute_schema": ingestAttributeSchemaSchema(),
		"project_id":       projectIDSchema,
	},
	Required: []string{"items"},
}

var ingestBatchOutputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"results":              {Type: "array"},
		"submitted_count":      {Type: "integer"},
		"failed_count":         {Type: "integer"},
		"quota_exceeded_count": {Type: "integer"},
		"scope":                scopeSchema,
		"error":                {Type: "string"},
		"code":                 errorCodeSchema,
		"retryable":            retryableSchema,
		"retry_after_seconds":  retryAfterSchema,
	},
}

var ingestBatchTool = &mcp.Tool{
	Name: "index4ai_ingest_batch",
	Annotations: &mcp.ToolAnnotations{
		Title:           "Ingest Documents (batch)",
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(true),
	},
	Description:  fmt.Sprintf("Ingest several documents into index4.ai in one call, with controlled upload concurrency (default %d, max %d). Accepts up to %d items, each a url or file_base64. Returns one result per item, in input order, immediately after the uploads finish: it does not wait for server-side processing; poll progress with index4ai_jobs_status. The server cannot read the caller's local files: for a folder of local files, either upload each with file_base64 or host them and pass urls. The response includes a `scope` object identifying which project the documents were ingested into. ALWAYS tell the user the project (scope.project_name / scope.hint). Free-tier accounts should set concurrency to 1. When the account is at its concurrent-job capacity the API returns too_many_jobs; those items come back with quota_exceed=true (counted in quota_exceeded_count): they were NOT ingested. Retry only the quota_exceed items once running jobs finish (poll index4ai_jobs_status); lower concurrency if it recurs. attributes and attribute_schema apply to every item.", ingestBatchDefaultConcurrent, ingestBatchMaxConcurrent, ingestBatchMaxItems) + attributesReuseGuidance + errorHandlingGuidance,
	InputSchema:  ingestBatchInputSchema,
	OutputSchema: ingestBatchOutputSchema,
}

type IngestBatchItem struct {
	URL        string `json:"url,omitempty"`
	FileBase64 string `json:"file_base64,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

type IngestBatchInput struct {
	Items       []IngestBatchItem `json:"items"`
	Token       string            `json:"token,omitempty"`
	Concurrency int               `json:"concurrency,omitempty"`
	ProjectID   string            `json:"project_id,omitempty"`
	// Attributes / AttributeSchema apply to EVERY item in the batch.
	Attributes      map[string]json.RawMessage `json:"attributes,omitempty"`
	AttributeSchema map[string]json.RawMessage `json:"attribute_schema,omitempty"`
}

type IngestBatchResult struct {
	// Index is the item's position in the request; Name is its url or filename.
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	JobID string `json:"job_id,omitempty"`
	// GrillError promotes error, code, retryable, retry_after_seconds to the top level.
	GrillError
	QuotaExceed bool `json:"quota_exceed,omitempty"`
}

type IngestBatchOutput struct {
	Results            []IngestBatchResult `json:"results"`
	SubmittedCount     int                 `json:"submitted_count"`
	FailedCount        int                 `json:"failed_count"`
	QuotaExceededCount int                 `json:"quota_exceeded_count"`
	Scope              *GrillScope         `json:"scope,omitempty"`
	// GrillError promotes error, code, retryable, retry_after_seconds to the top level.
	GrillError
}

func IngestBatch(ctx context.Context, _ *mcp.CallToolRequest, input IngestBatchInput) (*mcp.CallToolResult, IngestBatchOutput, error) {
	token := getToken(ctx, input.Token)
	if token == "" {
		return errResult(), IngestBatchOutput{Results: []IngestBatchResult{}, GrillError: errOut(CodeMissingToken, "token is required (set an API key as the Authorization: Bearer or x-api-key header in the MCP client config, or pass token)")}, nil
	}
	if len(input.Items) == 0 {
		return errResult(), IngestBatchOutput{Results: []IngestBatchResult{}, GrillError: errOut(CodeInvalidInput, "items is required")}, nil
	}
	if len(input.Items) > ingestBatchMaxItems {
		return errResult(), IngestBatchOutput{Results: []IngestBatchResult{}, GrillError: errOut(CodeInvalidInput, "items exceeds limit of %d", ingestBatchMaxItems)}, nil
	}

	concurrency := input.Concurrency
	if concurrency <= 0 {
		concurrency = ingestBatchDefaultConcurrent
	}
	if concurrency > ingestBatchMaxConcurrent {
		concurrency = ingestBatchMaxConcurrent
	}

	// Validate once, before any upload: a bad attribute set fails the whole
	// batch rather than every item individually.
	meta, aerr := encodeIngestMeta(nil, labelsSourceArg, input.Attributes, input.AttributeSchema)
	if aerr != nil {
		return errResult(), IngestBatchOutput{Results: []IngestBatchResult{}, GrillError: errOut(CodeInvalidInput, "%s", aerr.Error())}, nil
	}

	c := grillClient(token)
	results := make([]IngestBatchResult, len(input.Items))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, it := range input.Items {
		wg.Add(1)
		go func(i int, it IngestBatchItem) {
			sem <- struct{}{}
			defer wg.Done()
			defer func() { <-sem }()

			name := it.URL
			if name == "" {
				name = it.Filename
			}
			r := IngestBatchResult{Index: i, Name: name}
			r.JobID, r.GrillError = submitIngest(ctx, c, GrillIngestInput{
				URL: it.URL, FileBase64: it.FileBase64, Filename: it.Filename, Token: input.Token,
			}, input.ProjectID, meta)
			r.QuotaExceed = r.Code == CodeTooManyJobs
			results[i] = r
		}(i, it)
	}
	wg.Wait()

	out := IngestBatchOutput{Results: results}
	for _, r := range results {
		switch {
		case r.JobID != "":
			out.SubmittedCount++
		case r.QuotaExceed:
			out.QuotaExceededCount++
		default:
			out.FailedCount++
		}
	}
	if out.SubmittedCount == 0 && out.QuotaExceededCount == 0 {
		// Every item failed: surface the first item's classification at the top
		// level so `code` is never empty; per-item results stay authoritative.
		first := results[0].GrillError
		out.GrillError = GrillError{Error: fmt.Sprintf("all %d item(s) failed to submit", len(results)), Code: first.Code, Retryable: first.Retryable, RetryAfterSeconds: first.RetryAfterSeconds}
		return errResult(), out, nil
	}
	_, source := projectIDSource(token, input.ProjectID)
	out.Scope = resolveScope(c, token, input.ProjectID, "", source)
	return nil, out, nil
}
