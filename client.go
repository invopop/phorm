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
// POST /api/validate/{vesid}. A non-nil error indicates a transport/HTTP-level
// failure (unreachable service, bad token, malformed XML rejected with 400,
// etc.); validation findings are carried in the returned response.
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

	body, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}

	var pr phormValidationResult
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("phorm: decoding validate response: %w", err)
	}
	return pr.toResponse(), nil
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

	body, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}

	// phorm returns a bare JSON array of VESID objects.
	var items []phormVesID
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("phorm: decoding vesids response: %w", err)
	}

	filter := strings.ToLower(req.Filter)
	resp := &ListVesIdsResponse{}
	for _, it := range items {
		if filter != "" &&
			!strings.Contains(strings.ToLower(it.ID), filter) &&
			!strings.Contains(strings.ToLower(it.DisplayName), filter) {
			continue
		}
		resp.Vesids = append(resp.Vesids, it.toInfo())
	}
	return resp, nil
}

// do executes the request and returns the response body, converting non-2xx
// statuses into errors.
func (c *Client) do(req *http.Request) ([]byte, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("phorm: request to %s: %w", req.URL, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("phorm: reading response from %s: %w", req.URL, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("phorm: %s %s: unexpected status %s: %s",
			req.Method, req.URL, res.Status, truncate(body, 512))
	}
	return body, nil
}

// --- phorm JSON wire types (phive's JsonValidationResultListHelper schema) ---

type phormValidationResult struct {
	Success flexBool `json:"success"`
	Ves     *struct {
		VesID string `json:"vesid"`
	} `json:"ves"`
	Results []phormLayerResult `json:"results"`
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

type phormVesID struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Deprecated  bool   `json:"deprecated"`
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

func (v phormVesID) toInfo() *VesIdInfo {
	status := "VALID"
	if v.Deprecated {
		status = "DEPRECATED"
	}
	return &VesIdInfo{
		Vesid:  v.ID,
		Name:   v.DisplayName,
		Status: status,
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
