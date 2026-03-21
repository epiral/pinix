// Role:    Shared daemon response and parameter types for HubService and provider routing
// Depends: fmt
// Exports: ResponseError, AddParams, RemoveParams, AddResult, RemoveResult

package daemon

import (
	"fmt"
)

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
	Name   string `json:"name,omitempty"`
	Token  string `json:"token,omitempty"`
}

type RemoveParams struct {
	Name string `json:"name"`
}

type AddResult struct {
	Clip ClipConfig `json:"clip"`
}

type RemoveResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
