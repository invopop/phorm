// Package phorm is a Go HTTP client for a phorm (https://github.com/phax/phorm)
// business-document validation service. It replaces the gRPC client of the
// invopop/phive service; the request/response types keep the same exported field
// names so callers migrate by swapping only the constructor.
package phorm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout = 60 * time.Second
	// tokenHeader is phorm's authentication header (phorm.api.requiredtoken).
	tokenHeader = "X-Token"
	// DefaultToken is phorm's built-in default X-Token. phorm always requires a
	// matching non-empty token and cannot disable auth, but when it is only
	// reachable inside a trusted network the token is not a security boundary, so
	// New falls back to this when no token is supplied.
	DefaultToken = "phorm-dev-token"
)

// Client talks to a phorm validation service over HTTP.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (e.g. to set a custom
// timeout or transport).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New creates a phorm client for the service at baseURL (e.g.
// "http://phorm:8080") authenticating with the given X-Token value. An empty
// token falls back to DefaultToken, so callers pointing at a phorm that uses the
// stock token need not configure one.
func New(baseURL, token string, opts ...Option) *Client {
	if token == "" {
		token = DefaultToken
	}
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ValidateXml validates req.XmlContent against req.Vesid via
// POST /api/validate/{vesid}.
//
// phorm answers a document that breaks a rule with HTTP 400 and the validation
// report as the body, so the status code alone cannot tell a failed validation
// from a rejected request: an unresolvable VESID and a body that is not XML are
// also 400, and a bad token is 403. What separates them is the body, which is
// the JSON report only when the validation actually ran.
//
// A report is therefore returned as a response whatever the status, with the
// findings in resp.Results and resp.Success reporting the outcome. A non-nil
// error means the request never produced a report — the service is unreachable,
// the token was rejected, the VESID could not be resolved, or the document was
// not readable as XML.
func (c *Client) ValidateXml(ctx context.Context, req *ValidateXmlRequest) (*ValidateXmlResponse, error) {
	endpoint := c.baseURL + "/api/validate/" + url.PathEscape(req.Vesid)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(req.XmlContent))
	if err != nil {
		return nil, fmt.Errorf("phorm: building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/xml")
	httpReq.Header.Set("Accept", "application/json")
	if c.token != "" {
		httpReq.Header.Set(tokenHeader, c.token)
	}

	res, body, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}

	var pr phormValidationResult
	jsonErr := json.Unmarshal(body, &pr)

	// A 2xx from the validate endpoint is always the report.
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if jsonErr != nil {
			return nil, fmt.Errorf("phorm: %s %s: decoding validate response: %w",
				httpReq.Method, httpReq.URL, jsonErr)
		}
		return pr.toResponse(), nil
	}

	// Otherwise the status is ambiguous, and only a body that carries the
	// report is a validation that ran and failed.
	if jsonErr == nil && pr.isReport() {
		return pr.toResponse(), nil
	}
	return nil, fmt.Errorf("phorm: %s %s: %s: %s",
		httpReq.Method, httpReq.URL, res.Status, truncate(body, 512))
}

// ListVesIds lists the available VESIDs via GET /api/get/vesids. req.Filter, if
// set, is applied client-side (phorm has no server-side filter).
func (c *Client) ListVesIds(ctx context.Context, req *ListVesIdsRequest) (*ListVesIdsResponse, error) {
	endpoint := c.baseURL + "/api/get/vesids?include-deprecated=true"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("phorm: building request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	if c.token != "" {
		httpReq.Header.Set(tokenHeader, c.token)
	}

	res, body, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("phorm: %s %s: %s: %s",
			httpReq.Method, httpReq.URL, res.Status, truncate(body, 512))
	}

	items, err := decodeVesIDs(body)
	if err != nil {
		return nil, err
	}

	filter := strings.ToLower(req.Filter)
	resp := &ListVesIdsResponse{}
	for _, it := range items {
		if filter != "" &&
			!strings.Contains(strings.ToLower(it.id()), filter) &&
			!strings.Contains(strings.ToLower(it.name()), filter) {
			continue
		}
		resp.Vesids = append(resp.Vesids, it.toInfo())
	}
	return resp, nil
}

// do executes the request and returns the response alongside its body. The
// status is left for the caller to interpret, because phorm uses 400 both for a
// rejected request and for a document that simply failed validation.
func (c *Client) do(req *http.Request) (*http.Response, []byte, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("phorm: request to %s: %w", req.URL, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("phorm: reading response from %s: %w", req.URL, err)
	}
	return res, body, nil
}

// --- phorm JSON wire types (phive's JsonValidationResultListHelper schema) ---

type phormValidationResult struct {
	Success flexBool `json:"success"`
	Ves     *struct {
		VesID string `json:"vesid"`
	} `json:"ves"`
	Results []phormLayerResult `json:"results"`
}

// isReport reports whether the decoded body is actually a validation report
// rather than some other JSON phorm happened to return. The validation layers
// and the resolved VES are the parts only a real report carries; `success`
// alone is not enough, as it decodes from any JSON object.
func (r phormValidationResult) isReport() bool {
	return len(r.Results) > 0 || r.Ves != nil
}

type phormLayerResult struct {
	Success      flexBool         `json:"success"`
	Validity     string           `json:"validity"`
	ArtifactType string           `json:"artifactType"`
	ArtifactPath string           `json:"artifactPath"`
	Items        []phormErrorItem `json:"items"`
}

// flexBool decodes a real boolean at the top level and phive's tri-state string
// ("TRUE"/"FALSE"/"UNDEFINED") on each result. Never fails: rejecting a value
// here would discard the whole response, findings included.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	*b = flexBool(strings.EqualFold(strings.Trim(string(data), `"`), "true"))
	return nil
}

type phormErrorItem struct {
	ErrorLevel     string `json:"errorLevel"`
	ErrorText      string `json:"errorText"`
	ErrorFieldName string `json:"errorFieldName"`
	Test           string `json:"test"`
	ErrorID        string `json:"errorID"`
	Location       *struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	} `json:"errorLocationObj"`
}

// phormVesID accepts both spellings of a VESID entry. phorm itself sends
// `vesid` and `name`; `id` and `displayName` are kept because earlier notes on
// the API described that shape, and tolerating both costs nothing.
type phormVesID struct {
	VesID       string `json:"vesid"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Deprecated  bool   `json:"deprecated"`
}

// decodeVesIDs reads the VESID list, which phorm wraps in an object alongside a
// count. A bare array is also accepted so that either shape decodes.
func decodeVesIDs(body []byte) ([]phormVesID, error) {
	var wrapped struct {
		Vesids []phormVesID `json:"vesids"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Vesids != nil {
		return wrapped.Vesids, nil
	}

	var items []phormVesID
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("phorm: decoding vesids response: %w", err)
	}
	return items, nil
}

func (r phormValidationResult) toResponse() *ValidateXmlResponse {
	out := &ValidateXmlResponse{Success: bool(r.Success)}
	if r.Ves != nil {
		out.ResolvedVesid = r.Ves.VesID
	}
	for _, lr := range r.Results {
		out.Results = append(out.Results, lr.toLayer())
	}
	return out
}

func (lr phormLayerResult) toLayer() *ValidationLayerResult {
	// Prefer the explicit validity flag when present; fall back to success.
	success := bool(lr.Success)
	if lr.Validity != "" {
		success = strings.EqualFold(lr.Validity, "valid")
	}
	layer := &ValidationLayerResult{
		ValidationType: lr.ArtifactType,
		ArtifactId:     lr.ArtifactPath,
		Success:        success,
	}
	// phorm returns a single `items` array; split it back into errors/warnings
	// by severity so callers that read .Errors and .Warnings keep working.
	for _, it := range lr.Items {
		switch strings.ToUpper(it.ErrorLevel) {
		case "WARN", "WARNING":
			layer.Warnings = append(layer.Warnings, it.toError())
		case "SUCCESS", "INFO":
			// informational only; not a finding.
		default: // ERROR, FATAL_ERROR, …
			layer.Errors = append(layer.Errors, it.toError())
		}
	}
	return layer
}

func (it phormErrorItem) toError() *ValidationError {
	loc := ""
	if it.Location != nil && (it.Location.Line > 0 || it.Location.Col > 0) {
		loc = "line " + strconv.Itoa(it.Location.Line)
		if it.Location.Col > 0 {
			loc += ", col " + strconv.Itoa(it.Location.Col)
		}
	}
	return &ValidationError{
		Level:    it.ErrorLevel,
		ErrorID:  it.ErrorID,
		Message:  it.ErrorText,
		Location: loc,
		Xpath:    it.ErrorFieldName,
		TestId:   it.Test,
	}
}

func (v phormVesID) id() string {
	if v.VesID != "" {
		return v.VesID
	}
	return v.ID
}

func (v phormVesID) name() string {
	if v.Name != "" {
		return v.Name
	}
	return v.DisplayName
}

func (v phormVesID) toInfo() *VesIdInfo {
	status := "VALID"
	if v.Deprecated {
		status = "DEPRECATED"
	}
	return &VesIdInfo{
		Vesid:   v.id(),
		Name:    v.name(),
		Version: v.Version,
		Status:  status,
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
