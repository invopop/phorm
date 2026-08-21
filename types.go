package phorm

// These types mirror the messages that were generated from the old invopop/phive
// validation.proto, so callers migrating off the phive gRPC client (gov-sa, cii,
// ubl, gov-fr, …) keep compiling after switching to phorm's HTTP/JSON API. Only
// the transport and the constructor change; the exported field names are
// identical to the old protobuf-generated structs.

// ListVesIdsRequest is a request to list the available VESIDs.
type ListVesIdsRequest struct {
	// Filter, when non-empty, keeps only VESIDs whose id or name contains it
	// (case-insensitive). phorm has no server-side filter, so it is applied
	// client-side.
	Filter string
}

// ListVesIdsResponse contains the list of available VESIDs.
type ListVesIdsResponse struct {
	Vesids       []*VesIdInfo
	ErrorMessage string
}

// VesIdInfo describes a single VESID.
type VesIdInfo struct {
	Vesid   string
	Name    string
	Version string
	// Status is "VALID" or "DEPRECATED" (derived from phorm's `deprecated` flag).
	Status string
}

// ValidateXmlRequest is a request to validate an XML document.
type ValidateXmlRequest struct {
	// Vesid is sent as the {vesid} path segment of POST /api/validate/{vesid}.
	Vesid string
	// XmlContent is the raw XML request body.
	XmlContent []byte
	// SourceIdentifier is retained for caller-side logging only; phorm's
	// validate endpoint does not accept it.
	SourceIdentifier string
}

// ValidateXmlResponse is the result of a validation.
type ValidateXmlResponse struct {
	Success bool
	// ResolvedVesid is populated from phorm's `ves.vesid` when present.
	ResolvedVesid string
	Results       []*ValidationLayerResult
	// ErrorMessage carries an execution-level error. With phorm these arrive as
	// non-2xx HTTP responses and are returned as a Go error instead, so this is
	// normally empty.
	ErrorMessage string
	Timestamp    string
}

// ValidationLayerResult holds the outcome of a single validation layer/artifact.
type ValidationLayerResult struct {
	// ValidationType is mapped from phorm's `artifactType`.
	ValidationType string
	// ArtifactId is mapped from phorm's `artifactPath`.
	ArtifactId string
	Success    bool
	Errors     []*ValidationError
	Warnings   []*ValidationError
}

// ValidationError is a single error or warning item.
type ValidationError struct {
	// Level is "ERROR", "WARN", etc. (phorm's `errorLevel`).
	Level string
	// ErrorID is the rule identifier (phorm's `errorID`), e.g. "UBL-CR-397".
	ErrorID string
	// Message is phorm's `errorText`.
	Message string
	// Location is a human-readable "line N, col M" from phorm's `errorLocationObj`.
	Location string
	// Xpath is phorm's `errorFieldName`.
	Xpath string
	// TestId is the schematron rule id (phorm's `test`).
	TestId  string
	Details map[string]string
}
