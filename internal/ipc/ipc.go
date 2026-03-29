// Role:    Typed NDJSON message definitions shared by pinixd and Clip processes
// Depends: encoding/json, errors, fmt, strings
// Exports: Message, Manifest, DependencySpec, Error, ErrClosed, MessageTypeRegister, MessageTypeRegistered, MessageTypeInvoke, MessageTypeResult, MessageTypeError, MessageTypeChunk, MessageTypeDone

package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrClosed = errors.New("ipc client closed")

const (
	MessageTypeRegister   = "register"
	MessageTypeRegistered = "registered"
	MessageTypeInvoke     = "invoke"
	MessageTypeResult     = "result"
	MessageTypeError      = "error"
	MessageTypeChunk      = "chunk"
	MessageTypeDone       = "done"
)

type Message struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	Alias    string          `json:"alias,omitempty"`
	Clip     string          `json:"clip,omitempty"`
	Command  string          `json:"command,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
	Manifest *Manifest       `json:"manifest,omitempty"`
}

type Manifest struct {
	Package      string                         `json:"package,omitempty"`
	Version      string                         `json:"version,omitempty"`
	Domain       string                         `json:"domain,omitempty"`
	Description  string                         `json:"description,omitempty"`
	Commands     json.RawMessage                `json:"commands,omitempty"`
	HasWeb       bool                           `json:"has_web,omitempty"`
	Dependencies Dependencies                   `json:"dependencies,omitempty"`
	Patterns     []string                       `json:"patterns,omitempty"`
	Entities     map[string]json.RawMessage     `json:"entities,omitempty"`
}

type Dependencies map[string]DependencySpec

type DependencySpec struct {
	Package string `json:"package,omitempty"`
	Version string `json:"version,omitempty"`
}

type Error struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}
