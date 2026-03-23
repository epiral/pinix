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
	Package      string                    `json:"package,omitempty"`
	Version      string                    `json:"version,omitempty"`
	Domain       string                    `json:"domain,omitempty"`
	Description  string                    `json:"description,omitempty"`
	Commands     json.RawMessage           `json:"commands,omitempty"`
	Dependencies flexDependencies          `json:"dependencies,omitempty"`
}

// flexDependencies handles three wire formats for backward compatibility:
//   - map[string]DependencySpec  (new slot format)
//   - map[string]string          (old semver map)
//   - []string                   (legacy string array)
type flexDependencies map[string]DependencySpec

func (d *flexDependencies) UnmarshalJSON(data []byte) error {
	// Try new slot format first
	var specMap map[string]DependencySpec
	if err := json.Unmarshal(data, &specMap); err == nil {
		*d = flexDependencies(specMap)
		return nil
	}

	// Try old semver map: {"browser": "^1.0.0"}
	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err == nil {
		result := make(flexDependencies, len(stringMap))
		for name, version := range stringMap {
			result[name] = DependencySpec{Package: name, Version: version}
		}
		*d = result
		return nil
	}

	// Try legacy string array: ["browser"]
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		result := make(flexDependencies, len(list))
		for _, name := range list {
			name = strings.TrimSpace(name)
			if name != "" {
				result[name] = DependencySpec{Package: name}
			}
		}
		*d = result
		return nil
	}

	return fmt.Errorf("dependencies: unsupported format")
}

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
