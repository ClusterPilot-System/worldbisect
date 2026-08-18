package v1

import "github.com/ClusterPilot-System/worldbisect/internal/model"

type CommandSpec = model.CommandSpec
type Oracle = model.Oracle
type Capture = model.Capture
type Analysis = model.Analysis
type Job = model.Job
type APIError struct {
	Error ErrorBody `json:"error"`
}
type ErrorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details,omitempty"`
}
