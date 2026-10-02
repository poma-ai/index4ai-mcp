package tools

// index4ai HTTP client wrappers around (*client.Client).Do / DoJSON.
//
// Paths are relative to apiBaseURL() (https://api.index4.ai/v1). index4ai serves the
// retrieval product at the root: /v1/ingest is what /api/v4/grill/ingest is on the POMA
// API (poma-services-go docs/api/index4ai/v1/openapi.yaml).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/poma-ai/poma-cli/pkg/client"
)

// grillIngestData sends POST /ingest with raw file bytes.
func grillIngestData(c *client.Client, data []byte, filename string, projectID string, meta ingestHeaders) ([]byte, int, error) {
	name := grillSanitizeFilename(filename)
	headers := map[string]string{
		"Content-Disposition": `attachment; filename="` + name + `"`,
		"Content-Type":        "application/octet-stream",
		"Content-Length":      strconv.Itoa(len(data)),
	}
	if projectID != "" {
		headers["X-Project-ID"] = projectID
	}
	meta.apply(headers)
	return c.Do(http.MethodPost, "/ingest", bytes.NewReader(data), headers)
}

// grillIngestURL sends POST /ingest with an X-Remote-URL header and no
// body: the index4ai server fetches and ingests the remote URL. Returns the
// same {job_id} shape as grillIngestData.
func grillIngestURL(c *client.Client, remoteURL, projectID string, meta ingestHeaders) ([]byte, int, error) {
	headers := map[string]string{
		"X-Remote-URL": remoteURL,
	}
	if projectID != "" {
		headers["X-Project-ID"] = projectID
	}
	meta.apply(headers)
	return c.Do(http.MethodPost, "/ingest", nil, headers)
}

// grillSearch sends POST /search.
func grillSearch(c *client.Client, req grillSearchRequest, projectID string) ([]byte, int, error) {
	if projectID == "" {
		return c.DoJSON(http.MethodPost, "/search", req)
	}
	return doJSONWithProjectID(c, http.MethodPost, "/search", req, projectID)
}

// grillSearchInDoc sends POST /searchInDoc.
func grillSearchInDoc(c *client.Client, req grillSearchInDocRequest, projectID string) ([]byte, int, error) {
	if projectID == "" {
		return c.DoJSON(http.MethodPost, "/searchInDoc", req)
	}
	return doJSONWithProjectID(c, http.MethodPost, "/searchInDoc", req, projectID)
}

// doJSONWithProjectID marshals body as JSON and calls c.Do with an X-Project-ID header.
func doJSONWithProjectID(c *client.Client, method, endpoint string, body any, projectID string) ([]byte, int, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	headers := map[string]string{
		"Content-Type": "application/json",
		"X-Project-ID": projectID,
	}
	return c.Do(method, endpoint, bytes.NewReader(b), headers)
}

// grillDocsPageLimit is sent as an explicit limit on every docs request. The
// value matches the server default, so it changes nothing about page shapes —
// but its PRESENCE keeps this paging-aware client out of grill's legacy mode,
// whose list_docs_legacy_truncated_total metric must only count old,
// limit-unaware callers.
const grillDocsPageLimit = 100

// grillListDocs sends GET /docs, optionally passing a pagination cursor
// (empty cursor = first page).
func grillListDocs(c *client.Client, projectID, cursor string) ([]byte, int, error) {
	var headers map[string]string
	if projectID != "" {
		headers = map[string]string{"X-Project-ID": projectID}
	}
	endpoint := "/docs?limit=" + strconv.Itoa(grillDocsPageLimit)
	if cursor != "" {
		endpoint += "&cursor=" + url.QueryEscape(cursor)
	}
	return c.Do(http.MethodGet, endpoint, nil, headers)
}

// grillListAttributes sends GET /attributes: the typed attribute names
// the project has declared, with their types and the per-project name cap.
// Same auth and X-Project-ID handling as grillListDocs.
func grillListAttributes(c *client.Client, projectID string) ([]byte, int, error) {
	var headers map[string]string
	if projectID != "" {
		headers = map[string]string{"X-Project-ID": projectID}
	}
	return c.Do(http.MethodGet, "/attributes", nil, headers)
}

// grillListProjects sends GET /projects (optionally filtered by product).
func grillListProjects(c *client.Client, product string) ([]byte, int, error) {
	p := "/projects"
	if product != "" {
		p += "?product=" + url.QueryEscape(product)
	}
	return c.Do(http.MethodGet, p, nil, nil)
}

// grillProjectInfo calls GET /projects/info, which returns the single project a
// project API key is bound to (same object shape as one /projects entry). The
// gateway answers 401 for any other credential.
func grillProjectInfo(c *client.Client) ([]byte, int, error) {
	return c.Do(http.MethodGet, "/projects/info", nil, nil)
}

// grillSanitizeFilename mirrors the sanitizeContentDispositionFilename logic in poma-cli.
func grillSanitizeFilename(name string) string {
	ext := path.Ext(name)
	if name == "" || name == "." || name == ".." {
		return "upload" + ext
	}
	if strings.ContainsAny(name, "\"\\\r\n\x00") {
		return "upload" + ext
	}
	return name
}

// grillSearchRequest is the JSON body for POST /search.
type grillSearchRequest struct {
	Query            string           `json:"query"`
	ExcludeDocIDs    []string         `json:"exclude_doc_ids,omitempty"`
	AttributeFilters []map[string]any `json:"attribute_filters,omitempty"`
	ReturnAssets     bool             `json:"return_assets,omitempty"`
	ReturnPageImages bool             `json:"return_page_images,omitempty"`
}

// grillSearchInDocRequest is the JSON body for POST /searchInDoc.
type grillSearchInDocRequest struct {
	Query            string           `json:"query"`
	DocFilter        string           `json:"doc_filter"`
	ExcludeDocIDs    []string         `json:"exclude_doc_ids,omitempty"`
	AttributeFilters []map[string]any `json:"attribute_filters,omitempty"`
	ReturnAssets     bool             `json:"return_assets,omitempty"`
	ReturnPageImages bool             `json:"return_page_images,omitempty"`
}
