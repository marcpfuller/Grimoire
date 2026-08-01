package api

// UI API response types.
// These are the shapes serialised to JSON for the web frontend.
// Run `tygo generate` from the repo root to regenerate web/src/api/generated.ts.

// CommandResponse is a single command entry returned by GET /api/sessions.
type CommandResponse struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	Timestamp string `json:"timestamp"` // RFC3339
	Duration  string `json:"duration"`  // human-readable, e.g. "1.2s"
	ExitCode  *int   `json:"exitCode"`
	PaneID    string `json:"paneId"`
	SyncedGW  bool   `json:"syncedGW"`
}

// SessionResponse groups commands under a named pane.
type SessionResponse struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Commands []CommandResponse `json:"commands"`
}

// TagResponse is a single tag entry returned by GET /api/tags.
type TagResponse struct {
	ID          string `json:"id"`
	CommandID   string `json:"commandId"`
	ShortID     string `json:"shortId"`
	Label       string `json:"label"`
	CommandText string `json:"commandText"`
	PaneID      string `json:"paneId"`
	CreatedAt   string `json:"createdAt"` // RFC3339
}

// HealthResponse is returned by GET /api/health.
type HealthResponse struct {
	Status   string `json:"status"`
	Errors   int    `json:"errors"`
	Warnings int    `json:"warnings"`
	Version  string `json:"version"`
}

// ConfigGroup is a flat key→value map for one config section.
// Values can be string, number, or boolean.
//
//tygo:alias ConfigValue = string | number | boolean
type ConfigGroup = map[string]any

// MetadataResponse is returned by GET /api/commands/:id/metadata.
type MetadataResponse struct {
	Operator      string   `json:"operator"`
	Tool          string   `json:"tool"`
	UserContext   string   `json:"userContext"`
	SourceIP      string   `json:"sourceIp"`
	DestinationIP string   `json:"destinationIp"`
	Description   string   `json:"description"`
	Comments      string   `json:"comments"`
	Tags          []string `json:"tags"`
}

// MetadataUpdateRequest is the body for PATCH /api/commands/:id/metadata.
type MetadataUpdateRequest struct {
	Operator      *string  `json:"operator,omitempty"`
	Tool          *string  `json:"tool,omitempty"`
	UserContext   *string  `json:"userContext,omitempty"`
	SourceIP      *string  `json:"sourceIp,omitempty"`
	DestinationIP *string  `json:"destinationIp,omitempty"`
	Description   *string  `json:"description,omitempty"`
	Comments      *string  `json:"comments,omitempty"`
	AddTags       []string `json:"addTags,omitempty"`
	RemoveTags    []string `json:"removeTags,omitempty"`
}

// ErrorResponse wraps a plain error message.
type ErrorResponse struct {
	Error string `json:"error"`
}
