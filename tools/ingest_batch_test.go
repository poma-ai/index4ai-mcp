package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func batchStub(t *testing.T, ingest http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index4ai/v1/ingest":
			ingest(w, r)
		case "/index4ai/v1/projects":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("POMA_API_BASE_URL", srv.URL)
	return srv
}

// Each item goes through the single-ingest path: url items carry X-Remote-URL,
// base64 items upload a file, attributes ride on every request, and results come
// back in input order.
func TestIngestBatchMixedItems(t *testing.T) {
	var n atomic.Int32
	batchStub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Attributes"); got != `{"year":2025}` {
			t.Errorf("X-Attributes = %q, want shared attributes on every item", got)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"job_id":"job-%d"}`, n.Add(1))
	})

	_, out, err := IngestBatch(context.Background(), nil, IngestBatchInput{
		Token:      "tok",
		Attributes: map[string]json.RawMessage{"year": []byte("2025")},
		Items: []IngestBatchItem{
			{URL: "https://example.com/a.pdf"},
			{FileBase64: base64.StdEncoding.EncodeToString([]byte("# hi")), Filename: "b.md"},
		},
	})
	if err != nil || out.Error != "" {
		t.Fatalf("err=%v out.Error=%q", err, out.Error)
	}
	if out.SubmittedCount != 2 || out.FailedCount != 0 || len(out.Results) != 2 {
		t.Fatalf("counts = %+v", out)
	}
	if out.Results[0].Name != "https://example.com/a.pdf" || out.Results[1].Name != "b.md" || out.Results[0].Index != 0 || out.Results[1].Index != 1 {
		t.Fatalf("results out of order or misnamed: %+v", out.Results)
	}
	if out.Results[0].JobID == "" || out.Results[1].JobID == "" {
		t.Fatalf("missing job ids: %+v", out.Results)
	}
}

// One bad item and one throttled item must not sink the batch: the bad item is
// invalid_input, the throttled one is quota_exceed, the rest are submitted.
func TestIngestBatchPartialFailureAndQuota(t *testing.T) {
	batchStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.Header.Get("X-Remote-URL"), "/throttled.pdf") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"too_many_jobs","retry_after_seconds":7}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"job_id":"ok-1"}`))
	})

	_, out, err := IngestBatch(context.Background(), nil, IngestBatchInput{
		Token: "tok",
		Items: []IngestBatchItem{
			{URL: "https://example.com/ok.pdf"},
			{URL: "https://example.com/throttled.pdf"},
			{FileBase64: "!!not base64!!", Filename: "x.pdf"},
			{URL: "https://example.com/x", FileBase64: "aGk="},
		},
	})
	if err != nil || out.Error != "" {
		t.Fatalf("partial success must not be a tool error: err=%v out.Error=%q", err, out.Error)
	}
	if out.SubmittedCount != 1 || out.QuotaExceededCount != 1 || out.FailedCount != 2 {
		t.Fatalf("counts = submitted %d quota %d failed %d", out.SubmittedCount, out.QuotaExceededCount, out.FailedCount)
	}
	if r := out.Results[1]; !r.QuotaExceed || r.Code != CodeTooManyJobs || !r.Retryable {
		t.Fatalf("throttled item = %+v", r)
	}
	if out.Results[2].Code != CodeInvalidInput || out.Results[3].Code != CodeInvalidInput {
		t.Fatalf("bad items = %+v / %+v", out.Results[2], out.Results[3])
	}
}

// When every item fails the call is an error with the first item's code at the top.
func TestIngestBatchAllFail(t *testing.T) {
	_, out, _ := IngestBatch(context.Background(), nil, IngestBatchInput{
		Token: "tok",
		Items: []IngestBatchItem{{FileBase64: "!!"}, {}},
	})
	if out.Error == "" || out.Code != CodeInvalidInput || out.FailedCount != 2 {
		t.Fatalf("out = %+v", out)
	}
}

func TestIngestBatchInputValidation(t *testing.T) {
	ctx := context.Background()
	if _, out, _ := IngestBatch(ctx, nil, IngestBatchInput{Items: []IngestBatchItem{{URL: "u"}}}); out.Code != CodeMissingToken {
		t.Fatalf("no token: code = %q", out.Code)
	}
	if _, out, _ := IngestBatch(ctx, nil, IngestBatchInput{Token: "t"}); out.Code != CodeInvalidInput {
		t.Fatalf("empty items: code = %q", out.Code)
	}
	if _, out, _ := IngestBatch(ctx, nil, IngestBatchInput{Token: "t", Items: make([]IngestBatchItem, ingestBatchMaxItems+1)}); out.Code != CodeInvalidInput {
		t.Fatalf("too many items: code = %q", out.Code)
	}
	_, out, _ := IngestBatch(ctx, nil, IngestBatchInput{Token: "t", Items: []IngestBatchItem{{URL: "u"}}, Attributes: map[string]json.RawMessage{"Bad Name": []byte(`"x"`)}})
	if out.Code != CodeInvalidInput || out.SubmittedCount != 0 {
		t.Fatalf("bad attributes must fail the whole batch before any upload: %+v", out)
	}
}

// AddTool panics on a schema the SDK rejects, so registering proves it is valid.
func TestIngestBatchRegisters(t *testing.T) {
	Register(mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil))
}

// Early validation errors must pass the SDK's output-schema validation: a nil
// results would marshal as null and surface as a JSON-RPC error instead of a
// classified result.
func TestIngestBatchValidationErrorsSurviveTransport(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	Register(server)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for name, args := range map[string]map[string]any{
		"no token":    {"items": []any{map[string]any{"url": "u"}}},
		"empty items": {"token": "t", "items": []any{}},
		"too many":    {"token": "t", "items": make([]any, ingestBatchMaxItems+1)},
		"bad attrs":   {"token": "t", "items": []any{map[string]any{"url": "u"}}, "attributes": map[string]any{"Bad Name": "x"}},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "index4ai_ingest_batch", Arguments: args})
		if err != nil {
			t.Fatalf("%s: rpc error instead of a classified result: %v", name, err)
		}
		if !res.IsError {
			t.Fatalf("%s: want IsError result", name)
		}
	}
}

// A legacy 403 quota_exceeded means exhausted credits, not job capacity: it must
// stay a terminal failure, never a retryable quota_exceed item.
func TestIngestBatchLegacyQuota403IsNotRetryable(t *testing.T) {
	batchStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"quota_exceeded"}`))
	})
	_, out, _ := IngestBatch(context.Background(), nil, IngestBatchInput{Token: "tok", Items: []IngestBatchItem{{URL: "https://example.com/a.pdf"}}})
	if r := out.Results[0]; out.FailedCount != 1 || r.QuotaExceed || r.Retryable {
		t.Fatalf("out = %+v", out)
	}
}

// Concurrency is clamped to the max, and an explicit 1 is honoured (serial).
func TestIngestBatchConcurrencyCap(t *testing.T) {
	var cur, peak atomic.Int32
	batchStub(t, func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"job_id":"j"}`))
	})
	items := make([]IngestBatchItem, 12)
	for i := range items {
		items[i] = IngestBatchItem{URL: fmt.Sprintf("https://example.com/%d.pdf", i)}
	}
	_, _, _ = IngestBatch(context.Background(), nil, IngestBatchInput{Token: "t", Items: items, Concurrency: 1})
	if peak.Load() != 1 {
		t.Fatalf("concurrency=1 peak = %d", peak.Load())
	}
	peak.Store(0)
	_, _, _ = IngestBatch(context.Background(), nil, IngestBatchInput{Token: "t", Items: items, Concurrency: 99})
	if peak.Load() > ingestBatchMaxConcurrent {
		t.Fatalf("peak %d exceeds cap %d", peak.Load(), ingestBatchMaxConcurrent)
	}
}
