// Role:    Shared daemon request/response protocol types for clips and capabilities
// Depends: encoding/json, fmt
// Exports: Request, SocketResponse, ResponseError, AddParams, RemoveParams, InvokeParams, CapabilityInvokeRequest, AddResult, RemoveResult, ListResult, CapabilityListResult, ClipStatus, CapabilityStatus

package daemon

import (
	"encoding/json"
	"fmt"
)

type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Token  string          `json:"token,omitempty"`
}

type SocketResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

type AddParams struct {
	Source string `json:"source"`
	Token  string `json:"token,omitempty"`
}

type RemoveParams struct {
	Name string `json:"name"`
}

type InvokeParams struct {
	Clip    string          `json:"clip"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
}

type CapabilityInvokeRequest struct {
	Capability string          `json:"capability"`
	Command    string          `json:"command"`
	Input      json.RawMessage `json:"input,omitempty"`
}

type AddResult struct {
	Clip ClipConfig `json:"clip"`
}

type RemoveResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type ListResult struct {
	Clips        []ClipStatus       `json:"clips"`
	Capabilities []CapabilityStatus `json:"capabilities,omitempty"`
}

type CapabilityListResult struct {
	Capabilities []CapabilityStatus `json:"capabilities"`
}

type ClipStatus struct {
	Name           string         `json:"name"`
	Source         string         `json:"source"`
	Path           string         `json:"path"`
	Running        bool           `json:"running"`
	TokenProtected bool           `json:"token_protected"`
	Manifest       *ManifestCache `json:"manifest,omitempty"`
}

type CapabilityStatus struct {
	Name     string   `json:"name"`
	Commands []string `json:"commands"`
	Online   bool     `json:"online"`
}
