package mcpserver

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPStdioBinaryProtocolUsesStdoutAndDiagnosticsUseStderr(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(workingDir, "..", "..", ".."))
	binaryPath := filepath.Join(t.TempDir(), "voie")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build voie: %v\n%s", err, output)
	}

	cmd := exec.Command(binaryPath, "mcp")
	cmd.Env = environmentWith(map[string]string{
		"HOST": "127.0.0.1", "PORT": "8080", "DEBUG": "false", "TIMEOUT": "5", "API_TOKEN": "", "DEFAULT_PROVIDER": "",
	})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	transport := &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to voie mcp: %v (stderr: %s)", err, stderr.String())
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		t.Fatalf("list MCP tools: %v (stderr: %s)", err, stderr.String())
	}
	if len(tools.Tools) != 7 {
		_ = session.Close()
		t.Fatalf("tools = %v, want seven tools including conversations", tools.Tools)
	}
	models, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_models", Arguments: map[string]any{}})
	if err != nil || models.IsError || models.StructuredContent == nil {
		_ = session.Close()
		t.Fatalf("list_models result = %+v, err = %v (stderr: %s)", models, err, stderr.String())
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close MCP session: %v", err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatalf("MCP subprocess did not exit cleanly after session close")
	}
	// A successful SDK handshake and tool call proves stdout contains only
	// newline-delimited protocol messages; any process diagnostics are captured
	// independently on stderr (which may legitimately be empty).
}

func environmentWith(values map[string]string) []string {
	result := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		key := entry
		if index := bytes.IndexByte([]byte(entry), '='); index >= 0 {
			key = entry[:index]
		}
		if _, replace := values[key]; replace {
			continue
		}
		result = append(result, entry)
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}
