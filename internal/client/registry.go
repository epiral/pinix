// Role:    HTTP client for Pinix Registry package, search, auth, and publish APIs
// Depends: bytes, context, encoding/json, fmt, io, mime/multipart, net/http, net/url, strings
// Exports: RegistryClient, NewRegistry, RegistryPackageDocument, RegistryVersionDocument, RegistryDistInfo, RegistrySearchResponse, RegistrySearchResult, RegistryPublishResponse, RegistryAuthResponse

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

type RegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

type RegistryDistInfo struct {
	Tarball string `json:"tarball,omitempty"`
	Shasum  string `json:"shasum,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

type RegistryVersionDocument struct {
	Pinix      json.RawMessage   `json:"pinix"`
	Dist       *RegistryDistInfo `json:"dist,omitempty"`
	Deprecated string            `json:"deprecated,omitempty"`
}

type RegistryPackageDocument struct {
	Name        string                             `json:"name"`
	Type        string                             `json:"type"`
	Description string                             `json:"description"`
	Domain      string                             `json:"domain,omitempty"`
	DistTags    map[string]string                  `json:"dist-tags"`
	Versions    map[string]RegistryVersionDocument `json:"versions"`
}

type RegistrySearchResponse struct {
	Results []RegistrySearchResult `json:"results"`
	Total   int                    `json:"total"`
}

type RegistrySearchResult struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Domain      string `json:"domain,omitempty"`
}

type RegistryPublishResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Tag     string `json:"tag"`
}

type RegistryAuthResponse struct {
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
}

type registryAPIError struct {
	Error string `json:"error"`
}

func NewRegistry(baseURL string) (*RegistryClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("registry URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse registry URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("registry URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("registry URL host is required")
	}
	return &RegistryClient{baseURL: baseURL, httpClient: http.DefaultClient}, nil
}

func (c *RegistryClient) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// splitScopedName splits "@scope/name" into ("scope", "name").
// If the name is not scoped, returns ("", name).
func splitScopedName(name string) (string, string) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "@") {
		return "", name
	}
	parts := strings.SplitN(name[1:], "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", name
	}
	return parts[0], parts[1]
}

func (c *RegistryClient) GetPackage(ctx context.Context, name string) (*RegistryPackageDocument, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("package name is required")
	}
	scope, shortName := splitScopedName(name)
	var path string
	if scope != "" {
		path = "/packages/" + url.PathEscape(scope) + "/" + url.PathEscape(shortName)
	} else {
		path = "/packages/" + url.PathEscape(name)
	}
	var doc RegistryPackageDocument
	if err := c.getJSON(ctx, path, &doc); err != nil {
		return nil, err
	}
	if doc.DistTags == nil {
		doc.DistTags = make(map[string]string)
	}
	if doc.Versions == nil {
		doc.Versions = make(map[string]RegistryVersionDocument)
	}
	return &doc, nil
}

func (d *RegistryPackageDocument) ResolveVersion(requested string) (string, *RegistryVersionDocument, error) {
	if d == nil {
		return "", nil, fmt.Errorf("registry package document is required")
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = strings.TrimSpace(d.DistTags["latest"])
	}
	if requested == "" {
		return "", nil, fmt.Errorf("package %q does not have a latest dist-tag", d.Name)
	}
	if version, ok := d.DistTags[requested]; ok {
		requested = strings.TrimSpace(version)
	}
	versionDoc, ok := d.Versions[requested]
	if !ok {
		return "", nil, fmt.Errorf("package %q version or dist-tag %q not found", d.Name, requested)
	}
	copy := versionDoc
	return requested, &copy, nil
}

func (c *RegistryClient) Search(ctx context.Context, query, domain, packageType string) (*RegistrySearchResponse, error) {
	values := url.Values{}
	values.Set("q", strings.TrimSpace(query))
	if strings.TrimSpace(domain) != "" {
		values.Set("domain", strings.TrimSpace(domain))
	}
	if strings.TrimSpace(packageType) != "" {
		values.Set("type", strings.TrimSpace(packageType))
	}
	var resp RegistrySearchResponse
	if err := c.getJSON(ctx, "/search?"+values.Encode(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Download fetches a tarball by scope/name/version from the registry.
func (c *RegistryClient) Download(ctx context.Context, name, version string) ([]byte, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" {
		return nil, fmt.Errorf("package name is required for download")
	}
	if version == "" {
		return nil, fmt.Errorf("package version is required for download")
	}
	scope, shortName := splitScopedName(name)
	var path string
	if scope != "" {
		path = "/packages/" + url.PathEscape(scope) + "/" + url.PathEscape(shortName) + "/" + url.PathEscape(version) + "/download"
	} else {
		path = "/packages/" + url.PathEscape(name) + "/" + url.PathEscape(version) + "/download"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build registry download request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download registry tarball: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.decodeAPIError(resp, "download registry tarball")
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read registry tarball: %w", err)
	}
	return data, nil
}

func (c *RegistryClient) Publish(ctx context.Context, name, token string, manifest json.RawMessage, tarball []byte, tag string) (*RegistryPublishResponse, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("package name is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("registry token is required")
	}
	if len(bytes.TrimSpace(manifest)) == 0 {
		return nil, fmt.Errorf("manifest is required")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("manifest", strings.TrimSpace(string(manifest))); err != nil {
		return nil, fmt.Errorf("write registry manifest field: %w", err)
	}
	if strings.TrimSpace(tag) != "" {
		if err := writer.WriteField("tag", strings.TrimSpace(tag)); err != nil {
			return nil, fmt.Errorf("write registry tag field: %w", err)
		}
	}
	if len(tarball) > 0 {
		fileName := name + ".tgz"
		part, err := writer.CreateFormFile("tarball", fileName)
		if err != nil {
			return nil, fmt.Errorf("create registry tarball field: %w", err)
		}
		if _, err := part.Write(tarball); err != nil {
			return nil, fmt.Errorf("write registry tarball field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close registry multipart body: %w", err)
	}

	scope, shortName := splitScopedName(name)
	var path string
	if scope != "" {
		path = "/packages/" + url.PathEscape(scope) + "/" + url.PathEscape(shortName) + "/versions"
	} else {
		path = "/packages/" + url.PathEscape(name) + "/versions"
	}

	var resp RegistryPublishResponse
	if err := c.doJSON(ctx, http.MethodPut, path, body, writer.FormDataContentType(), token, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *RegistryClient) Register(ctx context.Context, username, email, password string) (*RegistryAuthResponse, error) {
	payload := map[string]string{
		"username": strings.TrimSpace(username),
		"email":    strings.TrimSpace(email),
		"password": password,
	}
	var resp RegistryAuthResponse
	if err := c.postJSON(ctx, "/auth/register", payload, "", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *RegistryClient) Login(ctx context.Context, username, password string) (*RegistryAuthResponse, error) {
	payload := map[string]string{
		"username": strings.TrimSpace(username),
		"password": password,
	}
	var resp RegistryAuthResponse
	if err := c.postJSON(ctx, "/auth/login", payload, "", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *RegistryClient) WhoAmI(ctx context.Context, token string) (*RegistryAuthResponse, error) {
	var resp RegistryAuthResponse
	if err := c.doJSON(ctx, http.MethodGet, "/auth/whoami", nil, "application/json", token, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *RegistryClient) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, "application/json", "", out)
}

func (c *RegistryClient) postJSON(ctx context.Context, path string, payload any, token string, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal registry request body: %w", err)
	}
	return c.doJSON(ctx, http.MethodPost, path, bytes.NewReader(data), "application/json", token, out)
}

func (c *RegistryClient) doJSON(ctx context.Context, method, path string, body io.Reader, contentType, token string, out any) error {
	if c == nil {
		return fmt.Errorf("registry client is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build registry request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("registry %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.decodeAPIError(resp, fmt.Sprintf("registry %s %s", method, path))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode registry response: %w", err)
	}
	return nil
}

func (c *RegistryClient) decodeAPIError(resp *http.Response, action string) error {
	if resp == nil {
		return fmt.Errorf("%s: empty response", action)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	message := strings.TrimSpace(string(body))
	var apiErr registryAPIError
	if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
		message = strings.TrimSpace(apiErr.Error)
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s: %s", action, message)
}
