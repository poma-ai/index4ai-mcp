package tools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/poma-ai/poma-cli/pkg/client"
)

const defaultIngestMaxBytes int64 = 512 << 20 // 512 MiB

// ingestMaxBytes returns the max ingest payload size in bytes.
// GRILL_INGEST_MAX_BYTES: unset uses defaultIngestMaxBytes; "0" means unlimited.
func ingestMaxBytes() int64 {
	v := strings.TrimSpace(os.Getenv("GRILL_INGEST_MAX_BYTES"))
	if v == "" {
		return defaultIngestMaxBytes
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return defaultIngestMaxBytes
	}
	if n == 0 {
		return 0
	}
	return n
}

// resolveGrillIngestPayload returns the decoded file_base64 bytes and the
// effective basename for upload.
func resolveGrillIngestPayload(input GrillIngestInput) (data []byte, filename string, err error) {
	b64 := strings.TrimSpace(input.FileBase64)
	if b64 == "" {
		return nil, "", errors.New("one of file_base64 or url is required")
	}

	max := ingestMaxBytes()
	if max > 0 {
		// Upper bound on decoded size from base64 length (ignores whitespace).
		if est := int64(base64.StdEncoding.DecodedLen(len(b64))); est > max {
			return nil, "", fmt.Errorf("file_base64 decodes to more than GRILL_INGEST_MAX_BYTES (%d)", max)
		}
	}
	data, err = base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid file_base64: %w", err)
	}
	if len(data) == 0 {
		return nil, "", errors.New("decoded file_base64 is empty")
	}
	if max > 0 && int64(len(data)) > max {
		return nil, "", fmt.Errorf("decoded file exceeds GRILL_INGEST_MAX_BYTES (%d)", max)
	}

	filename = strings.TrimSpace(input.Filename)
	if filename == "" || filename == "." || filename == ".." {
		ext := guessExtensionFromContent(data)
		if ext == "" {
			return nil, "", errors.New("could not infer file extension: provide filename")
		}
		filename = "upload" + ext
	} else {
		filename = filepath.Base(filename)
	}

	return data, filename, nil
}

// uploadIngestMeta reads the caller's X-Labels, X-Attributes and
// X-Attribute-Schema headers off an /ingest-upload request and encodes them
// as the upstream ingest metadata, with the same validation the MCP tools
// apply to their arguments.
func uploadIngestMeta(r *http.Request) (ingestHeaders, error) {
	var attrs, schema map[string]json.RawMessage
	if v := strings.TrimSpace(r.Header.Get("X-Attributes")); v != "" {
		if err := json.Unmarshal([]byte(v), &attrs); err != nil || attrs == nil {
			return ingestHeaders{}, fmt.Errorf("X-Attributes must be a JSON object mapping attribute name to value")
		}
	}
	if v := strings.TrimSpace(r.Header.Get("X-Attribute-Schema")); v != "" {
		if err := json.Unmarshal([]byte(v), &schema); err != nil || schema == nil {
			return ingestHeaders{}, fmt.Errorf(`X-Attribute-Schema must be a JSON object like {"name": {"type": "encrypted_text"}}`)
		}
	}
	return encodeIngestMeta(labelItemsFromHeader(r.Header.Get("X-Labels")), labelsSourceHeader, attrs, schema)
}

// HandleIngestUpload serves POST /ingest-upload: raw body (octet-stream) or
// multipart field "file". Auth: same as MCP (x-api-key / Bearer).
// Filename: query filename=, header X-Filename, multipart filename, or upload.bin.
func HandleIngestUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := getToken(r.Context(), "")
	if token == "" {
		writeIngestUploadError(w, http.StatusUnauthorized, GrillError{
			Error: "missing API token (x-api-key or Authorization: Bearer)",
			Code:  CodeMissingToken,
		})
		return
	}

	// Caller metadata, validated before the body is read so bad headers are
	// refused without buffering the upload: a legacy X-Labels header is
	// translated into attributes.labels and merged with any X-Attributes /
	// X-Attribute-Schema the caller sent; only X-Attributes goes upstream,
	// never X-Labels (D157).
	meta, merr := uploadIngestMeta(r)
	if merr != nil {
		writeIngestUploadError(w, http.StatusBadRequest, GrillError{Error: merr.Error(), Code: CodeInvalidInput})
		return
	}

	max := ingestMaxBytes()
	if max > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, max)
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		mediaType = ""
	}

	var data []byte
	var filename string

	switch {
	case strings.HasPrefix(mediaType, "multipart/form-data"):
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeIngestUploadError(w, http.StatusBadRequest, GrillError{Error: err.Error(), Code: CodeInvalidInput})
			return
		}
		fh, hdr, err := r.FormFile("file")
		if err != nil {
			writeIngestUploadError(w, http.StatusBadRequest, GrillError{Error: "multipart form field \"file\" is required", Code: CodeInvalidInput})
			return
		}
		defer fh.Close()
		data, err = io.ReadAll(fh)
		if err != nil {
			writeIngestUploadError(w, http.StatusBadRequest, GrillError{Error: err.Error(), Code: CodeInvalidInput})
			return
		}
		if max > 0 && int64(len(data)) > max {
			writeIngestUploadError(w, http.StatusRequestEntityTooLarge, GrillError{Error: fmt.Sprintf("file exceeds GRILL_INGEST_MAX_BYTES (%d)", max), Code: CodeInvalidInput})
			return
		}
		filename = filepath.Base(hdr.Filename)
		if filename == "" || filename == "." {
			filename = "upload.bin"
		}
		if filename == "upload.bin" {
			if ext := guessExtensionFromContent(data); ext != "" {
				filename = "upload" + ext
			}
		}

	default:
		data, err = io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeIngestUploadError(w, http.StatusRequestEntityTooLarge, GrillError{Error: fmt.Sprintf("body exceeds GRILL_INGEST_MAX_BYTES (%d)", max), Code: CodeInvalidInput})
				return
			}
			writeIngestUploadError(w, http.StatusBadRequest, GrillError{Error: err.Error(), Code: CodeInvalidInput})
			return
		}
		if max > 0 && int64(len(data)) > max {
			writeIngestUploadError(w, http.StatusRequestEntityTooLarge, GrillError{Error: fmt.Sprintf("body exceeds GRILL_INGEST_MAX_BYTES (%d)", max), Code: CodeInvalidInput})
			return
		}
		filename = filepath.Base(strings.TrimSpace(r.URL.Query().Get("filename")))
		if filename == "" || filename == "." {
			filename = filepath.Base(strings.TrimSpace(r.Header.Get("X-Filename")))
		}
		if filename == "" || filename == "." {
			filename = "upload.bin"
		}
		if filename == "upload.bin" {
			if ext := guessExtensionFromContent(data); ext != "" {
				filename = "upload" + ext
			}
		}
	}

	if len(data) == 0 {
		writeIngestUploadError(w, http.StatusBadRequest, GrillError{Error: "empty body", Code: CodeInvalidInput})
		return
	}

	c := grillClient(token)
	projectID := r.Header.Get("X-Project-ID")
	body, st, err := grillIngestData(c, data, filename, projectID, meta)
	if err != nil {
		// Network/client error reaching the index4ai API — transient, retryable.
		writeIngestUploadError(w, http.StatusBadGateway, GrillError{Error: err.Error(), Code: CodeTransportError, Retryable: isRetryableCode(CodeTransportError, 0)})
		return
	}
	if authErr, authCode := interpretAuthError(r.Context(), "", st, body, "index4ai ingest"); authErr != "" {
		writeIngestUploadError(w, st, GrillError{Error: authErr, Code: authCode})
		return
	}
	if throttle, retryAfter, ok := interpretTooManyJobs(st, body); ok {
		writeIngestUploadError(w, st, GrillError{Error: throttle, Code: CodeTooManyJobs, Retryable: isRetryableCode(CodeTooManyJobs, 0), RetryAfterSeconds: retryAfter})
		return
	}
	if st != http.StatusCreated {
		writeIngestUploadError(w, st, GrillError{Error: fmt.Sprintf("index4ai ingest: %s", string(body)), Code: CodeUpstreamError, Retryable: isRetryableCode(CodeUpstreamError, st)})
		return
	}

	j, err := client.ParseJob(body)
	if err != nil || j.JobID == "" {
		writeIngestUploadError(w, http.StatusBadGateway, GrillError{Error: fmt.Sprintf("could not parse job_id from response: %s", string(body)), Code: CodeParseError})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": j.JobID})
}

// writeIngestUploadError writes a JSON error body for the HTTP upload endpoint.
// It reuses the GrillError envelope so HTTP clients get the same code/retryable/
// retry_after_seconds parity as the MCP tools instead of a bare prose message —
// branch on `code`, not the prose `error`.
func writeIngestUploadError(w http.ResponseWriter, httpStatus int, ge GrillError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(ge)
}

// defaultMCPMaxBodyBytes bounds an MCP JSON-RPC request body.
//
// This is deliberately far below GRILL_INGEST_MAX_BYTES. A base64 file inside a
// JSON-RPC message is buffered several times over before the bytes reach the
// index4ai API — the stateless HTTP path reads the body, re-buffers it, re-reads
// it, the tool arguments are unmarshalled and re-marshalled to apply schema
// defaults, and only then is the base64 decoded — so the peak cost is a
// multiple of the body size. Large files belong on POST /ingest-upload, which
// buffers once, or on url ingest, which never passes through this server.
const defaultMCPMaxBodyBytes int64 = 16 << 20 // 16 MiB => ~12 MiB of file

// MCPRequestBodyBytes returns the byte limit for an MCP HTTP request body, in
// the form expected by mcp.StreamableHTTPOptions.MaxRequestBodyBytes
// (0 = SDK default of 4 MiB, negative = unlimited).
//
// GRILL_MCP_MAX_BODY_BYTES overrides defaultMCPMaxBodyBytes; "0" means
// unlimited, which removes the only bound on how much a single request can
// make the server allocate. A GRILL_INGEST_MAX_BYTES small enough to make the
// default unreachable lowers the limit to match, so that an operator who caps
// ingest does not still accept oversized MCP bodies.
//
// The result is never 0: that would silently mean the SDK's 4 MiB default,
// which is too small for base64 ingest of even a modest document.
func MCPRequestBodyBytes() int64 {
	invalid := ""
	if v := strings.TrimSpace(os.Getenv("GRILL_MCP_MAX_BODY_BYTES")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		switch {
		case err != nil || n < 0:
			invalid = v
		case n == 0:
			return -1 // explicit operator opt-out
		default:
			return n
		}
	}
	limit := defaultMCPMaxBodyBytes
	if n := base64BodyBytes(ingestMaxBytes()); n > 0 && n < limit {
		limit = n // an ingest cap this small makes the default unreachable
	}
	if invalid != "" {
		slog.Warn("ignoring invalid GRILL_MCP_MAX_BODY_BYTES", "value", invalid, "using", limit)
	}
	return limit
}

// base64BodyBytes scales an ingest byte limit up by the base64 4/3 expansion and
// adds slack for the JSON-RPC envelope, giving the body size that carries a
// file of that limit. It returns -1 when the limit is unset or so large that the
// scaled value would not fit in an int64.
func base64BodyBytes(limit int64) int64 {
	const slack = 64 << 10
	if limit <= 0 || limit > (math.MaxInt64-slack)/4*3 {
		return -1
	}
	return limit/3*4 + slack
}
