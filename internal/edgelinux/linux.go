// Role:    Linux edge clip handler implementations (shell, docker, filesystem, system, network, process, package, cron)
// Depends: bytes, encoding/json, fmt, io, os, os/exec, path/filepath, strings
// Exports: (all handler functions used by router)

package edgelinux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func runCommand(name string, args ...string) (string, string, int, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", "", -1, err
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}

func jsonMarshal(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}
	return json.RawMessage(data), nil
}

func parseInput[T any](input json.RawMessage) (T, error) {
	var v T
	if len(input) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(input, &v); err != nil {
		return v, fmt.Errorf("parse input: %w", err)
	}
	return v, nil
}

func stringOrDefault(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return s
}

// ---------------------------------------------------------------------------
// 1. linux-shell
// ---------------------------------------------------------------------------

type shellExecInput struct {
	Command string `json:"command"`
	Dir     string `json:"dir,omitempty"`
}

type shellExecOutput struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func shellExec(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[shellExecInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Command) == "" {
		return nil, fmt.Errorf("command is required")
	}

	cmd := exec.Command("sh", "-c", params.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if params.Dir != "" {
		cmd.Dir = params.Dir
	}

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec: %w", err)
		}
	}

	return jsonMarshal(shellExecOutput{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	})
}

type shellExecScriptInput struct {
	Script   string `json:"script"`
	Language string `json:"language,omitempty"`
	Dir      string `json:"dir,omitempty"`
}

func shellExecScript(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[shellExecScriptInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Script) == "" {
		return nil, fmt.Errorf("script is required")
	}

	interpreter := stringOrDefault(params.Language, "sh")

	tmpFile, err := os.CreateTemp("", "edgescript-*")
	if err != nil {
		return nil, fmt.Errorf("create temp script: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(params.Script); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("write temp script: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command(interpreter, tmpFile.Name())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if params.Dir != "" {
		cmd.Dir = params.Dir
	}

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec script: %w", err)
		}
	}

	return jsonMarshal(shellExecOutput{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	})
}

// ---------------------------------------------------------------------------
// 2. linux-docker
// ---------------------------------------------------------------------------

type dockerLogsInput struct {
	Container string `json:"container"`
	Tail      int    `json:"tail,omitempty"`
}

type dockerRunInput struct {
	Image   string   `json:"image"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Detach  bool     `json:"detach,omitempty"`
	Remove  bool     `json:"rm,omitempty"`
	Name    string   `json:"name,omitempty"`
	Ports   []string `json:"ports,omitempty"`
	Env     []string `json:"env,omitempty"`
}

type dockerContainerInput struct {
	Container string `json:"container"`
}

func dockerPS(input json.RawMessage) (json.RawMessage, error) {
	stdout, stderr, exitCode, err := runCommand("docker", "ps", "--format", "json", "--no-trunc")
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	if exitCode != 0 {
		return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	containers := make([]json.RawMessage, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		containers = append(containers, json.RawMessage(line))
	}

	return jsonMarshal(map[string]any{"containers": containers})
}

func dockerImages(input json.RawMessage) (json.RawMessage, error) {
	stdout, stderr, exitCode, err := runCommand("docker", "images", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	if exitCode != 0 {
		return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	images := make([]json.RawMessage, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		images = append(images, json.RawMessage(line))
	}

	return jsonMarshal(map[string]any{"images": images})
}

func dockerLogs(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[dockerLogsInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Container) == "" {
		return nil, fmt.Errorf("container is required")
	}

	tail := "100"
	if params.Tail > 0 {
		tail = fmt.Sprintf("%d", params.Tail)
	}

	stdout, stderr, exitCode, err := runCommand("docker", "logs", params.Container, "--tail", tail)
	if err != nil {
		return nil, fmt.Errorf("docker logs: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

func dockerRun(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[dockerRunInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Image) == "" {
		return nil, fmt.Errorf("image is required")
	}

	args := []string{"run"}
	if params.Detach {
		args = append(args, "-d")
	}
	if params.Remove {
		args = append(args, "--rm")
	}
	if params.Name != "" {
		args = append(args, "--name", params.Name)
	}
	for _, port := range params.Ports {
		args = append(args, "-p", port)
	}
	for _, env := range params.Env {
		args = append(args, "-e", env)
	}
	args = append(args, params.Image)
	if params.Command != "" {
		args = append(args, params.Command)
	}
	args = append(args, params.Args...)

	stdout, stderr, exitCode, err := runCommand("docker", args...)
	if err != nil {
		return nil, fmt.Errorf("docker run: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

func dockerStop(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[dockerContainerInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Container) == "" {
		return nil, fmt.Errorf("container is required")
	}

	stdout, stderr, exitCode, err := runCommand("docker", "stop", params.Container)
	if err != nil {
		return nil, fmt.Errorf("docker stop: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

func dockerStart(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[dockerContainerInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Container) == "" {
		return nil, fmt.Errorf("container is required")
	}

	stdout, stderr, exitCode, err := runCommand("docker", "start", params.Container)
	if err != nil {
		return nil, fmt.Errorf("docker start: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

// ---------------------------------------------------------------------------
// 3. linux-filesystem
// ---------------------------------------------------------------------------

type fsSearchInput struct {
	Dir     string `json:"dir"`
	Pattern string `json:"pattern"`
}

type fsReadInput struct {
	Path string `json:"path"`
}

type fsWriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode,omitempty"`
}

type fsListInput struct {
	Dir string `json:"dir"`
}

func fsSearch(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[fsSearchInput](input)
	if err != nil {
		return nil, err
	}

	dir := stringOrDefault(params.Dir, ".")
	pattern := stringOrDefault(params.Pattern, "*")

	stdout, stderr, exitCode, err := runCommand("find", dir, "-name", pattern, "-maxdepth", "5")
	if err != nil {
		return nil, fmt.Errorf("find: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	return jsonMarshal(map[string]any{
		"files":    files,
		"count":    len(files),
		"exitCode": exitCode,
		"stderr":   stderr,
	})
}

func fsRead(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[fsReadInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}

	path := filepath.Clean(params.Path)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// Limit read size to 1MB
	const maxSize = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxSize))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	info, _ := f.Stat()
	size := int64(0)
	if info != nil {
		size = info.Size()
	}

	return jsonMarshal(map[string]any{
		"path":      path,
		"content":   string(data),
		"size":      size,
		"truncated": size > maxSize,
	})
}

func fsWrite(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[fsWriteInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}

	path := filepath.Clean(params.Path)
	mode := os.FileMode(0644)
	if params.Mode > 0 {
		mode = os.FileMode(params.Mode)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	if err := os.WriteFile(path, []byte(params.Content), mode); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	return jsonMarshal(map[string]any{
		"path":    path,
		"written": len(params.Content),
	})
}

func fsList(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[fsListInput](input)
	if err != nil {
		return nil, err
	}

	dir := stringOrDefault(params.Dir, ".")

	stdout, stderr, exitCode, err := runCommand("ls", "-la", dir)
	if err != nil {
		return nil, fmt.Errorf("ls: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

func fsDf(input json.RawMessage) (json.RawMessage, error) {
	stdout, stderr, exitCode, err := runCommand("df", "-h")
	if err != nil {
		return nil, fmt.Errorf("df: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

// ---------------------------------------------------------------------------
// 4. linux-system
// ---------------------------------------------------------------------------

func sysInfo(input json.RawMessage) (json.RawMessage, error) {
	hostname, _ := os.Hostname()

	result := map[string]any{
		"hostname": hostname,
	}

	if stdout, _, _, err := runCommand("uname", "-srm"); err == nil {
		result["kernel"] = strings.TrimSpace(stdout)
	}
	if stdout, _, _, err := runCommand("uname", "-o"); err == nil {
		result["os"] = strings.TrimSpace(stdout)
	}
	if stdout, _, _, err := runCommand("uname", "-m"); err == nil {
		result["arch"] = strings.TrimSpace(stdout)
	}
	if stdout, _, _, err := runCommand("uptime", "-p"); err == nil {
		result["uptime"] = strings.TrimSpace(stdout)
	} else if stdout2, _, _, err2 := runCommand("uptime"); err2 == nil {
		result["uptime"] = strings.TrimSpace(stdout2)
	}

	return jsonMarshal(result)
}

func sysCPU(input json.RawMessage) (json.RawMessage, error) {
	result := map[string]any{}

	// Try /proc/cpuinfo (Linux)
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		cores := 0
		model := ""
		for _, line := range lines {
			if strings.HasPrefix(line, "processor") {
				cores++
			}
			if strings.HasPrefix(line, "model name") && model == "" {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					model = strings.TrimSpace(parts[1])
				}
			}
		}
		result["cores"] = cores
		if model != "" {
			result["model"] = model
		}
	} else {
		// Fallback: nproc
		if stdout, _, _, err := runCommand("nproc"); err == nil {
			result["cores"] = strings.TrimSpace(stdout)
		}
	}

	// Try /proc/loadavg (Linux)
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		result["loadavg"] = strings.TrimSpace(string(data))
	} else {
		// Fallback: uptime for load
		if stdout, _, _, err := runCommand("uptime"); err == nil {
			result["loadavg"] = strings.TrimSpace(stdout)
		}
	}

	return jsonMarshal(result)
}

func sysMemory(input json.RawMessage) (json.RawMessage, error) {
	result := map[string]any{}

	// Try /proc/meminfo (Linux)
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		meminfo := map[string]string{}
		for _, line := range lines {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				meminfo[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
		result["meminfo"] = meminfo
	} else {
		// Fallback: free -h
		if stdout, _, _, err := runCommand("free", "-h"); err == nil {
			result["free"] = strings.TrimSpace(stdout)
		}
	}

	return jsonMarshal(result)
}

func sysProcesses(input json.RawMessage) (json.RawMessage, error) {
	// Try Linux ps format first, then fallback to simpler format
	stdout, stderr, exitCode, err := runCommand("ps", "aux", "--sort=-%cpu")
	if err != nil {
		// macOS fallback (no --sort flag)
		stdout, stderr, exitCode, err = runCommand("ps", "aux")
		if err != nil {
			return nil, fmt.Errorf("ps: %w", err)
		}
	}

	// Take top 20 lines (header + 19 processes)
	lines := strings.Split(stdout, "\n")
	if len(lines) > 21 {
		lines = lines[:21]
	}

	return jsonMarshal(map[string]any{
		"output":   strings.Join(lines, "\n"),
		"exitCode": exitCode,
		"stderr":   stderr,
	})
}

// ---------------------------------------------------------------------------
// 5. linux-network
// ---------------------------------------------------------------------------

func netInterfaces(input json.RawMessage) (json.RawMessage, error) {
	// Try ip addr (Linux), fallback to ifconfig
	stdout, stderr, exitCode, err := runCommand("ip", "addr", "show")
	if err != nil {
		stdout, stderr, exitCode, err = runCommand("ifconfig")
		if err != nil {
			return nil, fmt.Errorf("network interfaces: %w", err)
		}
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

func netConnections(input json.RawMessage) (json.RawMessage, error) {
	// Try ss (Linux), fallback to netstat
	stdout, stderr, exitCode, err := runCommand("ss", "-tuln")
	if err != nil {
		stdout, stderr, exitCode, err = runCommand("netstat", "-tuln")
		if err != nil {
			return nil, fmt.Errorf("network connections: %w", err)
		}
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

type netPingInput struct {
	Host  string `json:"host"`
	Count int    `json:"count,omitempty"`
}

func netPing(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[netPingInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Host) == "" {
		return nil, fmt.Errorf("host is required")
	}

	count := "4"
	if params.Count > 0 {
		count = fmt.Sprintf("%d", params.Count)
	}

	stdout, stderr, exitCode, err := runCommand("ping", "-c", count, params.Host)
	if err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

type netCurlInput struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

func netCurl(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[netCurlInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.URL) == "" {
		return nil, fmt.Errorf("url is required")
	}

	args := []string{"-s", "-S", "-w", "\n%{http_code}"}
	method := stringOrDefault(params.Method, "GET")
	args = append(args, "-X", strings.ToUpper(method))

	for key, value := range params.Headers {
		args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
	}
	if params.Body != "" {
		args = append(args, "-d", params.Body)
	}
	args = append(args, params.URL)

	stdout, stderr, exitCode, err := runCommand("curl", args...)
	if err != nil {
		return nil, fmt.Errorf("curl: %w", err)
	}

	// Parse status code from last line
	parts := strings.Split(strings.TrimSpace(stdout), "\n")
	statusCode := ""
	body := stdout
	if len(parts) > 1 {
		statusCode = parts[len(parts)-1]
		body = strings.Join(parts[:len(parts)-1], "\n")
	}

	return jsonMarshal(map[string]any{
		"statusCode": statusCode,
		"body":       body,
		"stderr":     stderr,
		"exitCode":   exitCode,
	})
}

// ---------------------------------------------------------------------------
// 6. linux-process
// ---------------------------------------------------------------------------

func procList(input json.RawMessage) (json.RawMessage, error) {
	stdout, stderr, exitCode, err := runCommand("ps", "aux")
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

type procKillInput struct {
	PID    int    `json:"pid"`
	Signal string `json:"signal,omitempty"`
}

func procKill(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[procKillInput](input)
	if err != nil {
		return nil, err
	}
	if params.PID <= 0 {
		return nil, fmt.Errorf("pid is required and must be positive")
	}

	signal := stringOrDefault(params.Signal, "TERM")
	stdout, stderr, exitCode, err := runCommand("kill", fmt.Sprintf("-%s", signal), fmt.Sprintf("%d", params.PID))
	if err != nil {
		return nil, fmt.Errorf("kill: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

type procServiceInput struct {
	Service string `json:"service"`
}

func procSystemdStatus(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[procServiceInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Service) == "" {
		return nil, fmt.Errorf("service is required")
	}

	stdout, stderr, exitCode, err := runCommand("systemctl", "status", params.Service)
	if err != nil {
		// systemctl status returns non-zero for stopped services; that's expected
		return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

func procSystemdRestart(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[procServiceInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Service) == "" {
		return nil, fmt.Errorf("service is required")
	}

	stdout, stderr, exitCode, err := runCommand("systemctl", "restart", params.Service)
	if err != nil {
		return nil, fmt.Errorf("systemctl restart: %w", err)
	}

	return jsonMarshal(shellExecOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr})
}

// ---------------------------------------------------------------------------
// 7. linux-package
// ---------------------------------------------------------------------------

func detectPackageManager() string {
	if _, err := exec.LookPath("apt"); err == nil {
		return "apt"
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "yum"
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf"
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		return "pacman"
	}
	if _, err := exec.LookPath("brew"); err == nil {
		return "brew"
	}
	return ""
}

func pkgList(input json.RawMessage) (json.RawMessage, error) {
	pm := detectPackageManager()

	var stdout, stderr string
	var exitCode int
	var err error

	switch pm {
	case "apt":
		stdout, stderr, exitCode, err = runCommand("dpkg", "-l")
	case "yum", "dnf":
		stdout, stderr, exitCode, err = runCommand("rpm", "-qa")
	case "pacman":
		stdout, stderr, exitCode, err = runCommand("pacman", "-Q")
	case "brew":
		stdout, stderr, exitCode, err = runCommand("brew", "list", "--versions")
	default:
		return nil, fmt.Errorf("no supported package manager found")
	}
	if err != nil {
		return nil, fmt.Errorf("package list: %w", err)
	}

	return jsonMarshal(map[string]any{
		"packageManager": pm,
		"output":         stdout,
		"exitCode":       exitCode,
		"stderr":         stderr,
	})
}

type pkgInstallInput struct {
	Package string `json:"package"`
}

func pkgInstall(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[pkgInstallInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Package) == "" {
		return nil, fmt.Errorf("package is required")
	}

	pm := detectPackageManager()
	var stdout, stderr string
	var exitCode int

	switch pm {
	case "apt":
		stdout, stderr, exitCode, err = runCommand("apt", "install", "-y", params.Package)
	case "yum":
		stdout, stderr, exitCode, err = runCommand("yum", "install", "-y", params.Package)
	case "dnf":
		stdout, stderr, exitCode, err = runCommand("dnf", "install", "-y", params.Package)
	case "pacman":
		stdout, stderr, exitCode, err = runCommand("pacman", "-S", "--noconfirm", params.Package)
	case "brew":
		stdout, stderr, exitCode, err = runCommand("brew", "install", params.Package)
	default:
		return nil, fmt.Errorf("no supported package manager found")
	}
	if err != nil {
		return nil, fmt.Errorf("package install: %w", err)
	}

	return jsonMarshal(map[string]any{
		"packageManager": pm,
		"package":        params.Package,
		"output":         stdout,
		"exitCode":       exitCode,
		"stderr":         stderr,
	})
}

type pkgSearchInput struct {
	Query string `json:"query"`
}

func pkgSearch(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[pkgSearchInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	pm := detectPackageManager()
	var stdout, stderr string
	var exitCode int

	switch pm {
	case "apt":
		stdout, stderr, exitCode, err = runCommand("apt", "search", params.Query)
	case "yum":
		stdout, stderr, exitCode, err = runCommand("yum", "search", params.Query)
	case "dnf":
		stdout, stderr, exitCode, err = runCommand("dnf", "search", params.Query)
	case "pacman":
		stdout, stderr, exitCode, err = runCommand("pacman", "-Ss", params.Query)
	case "brew":
		stdout, stderr, exitCode, err = runCommand("brew", "search", params.Query)
	default:
		return nil, fmt.Errorf("no supported package manager found")
	}
	if err != nil {
		return nil, fmt.Errorf("package search: %w", err)
	}

	return jsonMarshal(map[string]any{
		"packageManager": pm,
		"query":          params.Query,
		"output":         stdout,
		"exitCode":       exitCode,
		"stderr":         stderr,
	})
}

// ---------------------------------------------------------------------------
// 8. linux-cron
// ---------------------------------------------------------------------------

func cronList(input json.RawMessage) (json.RawMessage, error) {
	stdout, stderr, exitCode, err := runCommand("crontab", "-l")
	if err != nil {
		// crontab -l returns non-zero when there is no crontab
		return jsonMarshal(map[string]any{
			"entries":  []string{},
			"raw":     stderr,
			"exitCode": exitCode,
		})
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	entries := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}

	return jsonMarshal(map[string]any{
		"entries":  entries,
		"raw":      stdout,
		"exitCode": exitCode,
	})
}

type cronAddInput struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

func cronAdd(input json.RawMessage) (json.RawMessage, error) {
	params, err := parseInput[cronAddInput](input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(params.Schedule) == "" {
		return nil, fmt.Errorf("schedule is required")
	}
	if strings.TrimSpace(params.Command) == "" {
		return nil, fmt.Errorf("command is required")
	}

	entry := fmt.Sprintf("%s %s", params.Schedule, params.Command)

	// Get current crontab
	existingStdout, _, _, _ := runCommand("crontab", "-l")
	existing := strings.TrimSpace(existingStdout)

	var newCrontab string
	if existing != "" {
		newCrontab = existing + "\n" + entry + "\n"
	} else {
		newCrontab = entry + "\n"
	}

	// Write via pipe to crontab
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("crontab: %w", err)
		}
	}

	return jsonMarshal(map[string]any{
		"added":    entry,
		"exitCode": exitCode,
		"stderr":   stderr.String(),
	})
}
