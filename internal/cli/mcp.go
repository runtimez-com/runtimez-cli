package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// mcpPath is the hosted MCP endpoint on eac (Streamable HTTP, stateless JSON responses).
const mcpPath = "/eac/mcp"

// mcpProtocolHeader is echoed back once the server has negotiated a version at initialize.
const mcpProtocolHeader = "MCP-Protocol-Version"

// JSON-RPC error codes the bridge itself emits (server errors are forwarded untouched).
const (
	mcpErrUnauthorized = -32001
	mcpErrTransport    = -32603
)

// mcpMaxLine bounds one JSON-RPC message on stdin; a manifest for
// runtimez_readiness_evaluate_manifest is the largest legitimate payload (256 KB server-side).
const mcpMaxLine = 4 << 20

func newMcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the Runtimez MCP server over stdio (Claude Code, Cursor, any MCP client)",
		Long: `Bridge stdio JSON-RPC to the hosted Runtimez MCP endpoint using this CLI's credential.

There is no tool logic here: every message is forwarded to POST ` + mcpPath + ` on the
configured API and the answer is written back, so the catalog is exactly the hosted
server's. A --cluster (or the selected context's cluster) is injected as the default
"cluster" argument of tools/call when the client did not pass one.

  claude mcp add runtimez -- rtz mcp
  claude mcp add runtimez -- rtz mcp --context prod --cluster prod-eu`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			b := &mcpBridge{
				post: func(ctx context.Context, headers map[string]string, body []byte) (int, []byte, error) {
					resp, err := e.client.PostRaw(ctx, mcpPath, headers, body)
					if err != nil {
						return 0, nil, err
					}
					return resp.Status, resp.Body, nil
				},
				defaultCluster: e.cluster,
				out:            cmd.OutOrStdout(),
				errOut:         cmd.ErrOrStderr(),
			}
			return b.serve(cmd.Context(), cmd.InOrStdin())
		},
	}
}

// mcpBridge forwards newline-delimited JSON-RPC messages to the hosted endpoint.
type mcpBridge struct {
	post            func(ctx context.Context, headers map[string]string, body []byte) (int, []byte, error)
	defaultCluster  string
	out             io.Writer
	errOut          io.Writer
	protocolVersion string
}

func (b *mcpBridge) serve(ctx context.Context, in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), mcpMaxLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := b.handle(ctx, line); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

// handle forwards one message. It returns an error only for a broken stdout; every protocol
// or transport failure is answered on stdout as a JSON-RPC error so the client can recover.
func (b *mcpBridge) handle(ctx context.Context, line []byte) error {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return b.write(jsonRPCError(nil, -32700, "invalid JSON on stdin", nil))
	}
	id, hasID := msg["id"]
	method := strings.Trim(string(msg["method"]), `"`)

	if b.defaultCluster != "" && method == "tools/call" {
		if patched, ok := injectCluster(line, b.defaultCluster); ok {
			line = patched
		}
	}

	headers := map[string]string{mcpProtocolHeader: b.protocolVersion}
	status, body, err := b.post(ctx, headers, line)
	if err != nil {
		fmt.Fprintf(b.errOut, "rtz mcp: %v\n", err)
		if !hasID {
			return nil
		}
		return b.write(jsonRPCError(id, mcpErrTransport, "runtimez API unreachable: "+err.Error(), nil))
	}

	switch {
	case status == http.StatusAccepted, status == http.StatusNoContent:
		return nil // notification or client response acknowledged; nothing to relay
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		fmt.Fprintf(b.errOut, "rtz mcp: HTTP %d from %s — run `rtz login` or set RTZ_TOKEN to an api:read key\n", status, mcpPath)
		if !hasID {
			return nil
		}
		return b.write(jsonRPCError(id, mcpErrUnauthorized,
			"not authorized: run `rtz login` or set RTZ_TOKEN to an API key with the api:read scope", nil))
	case status >= 200 && status < 300:
		if method == "initialize" {
			b.protocolVersion = negotiatedVersion(body)
		}
		return b.write(body)
	default:
		if json.Valid(body) && bytes.Contains(body, []byte(`"jsonrpc"`)) {
			return b.write(body) // the server already answered in JSON-RPC (400s do)
		}
		if !hasID {
			return nil
		}
		return b.write(jsonRPCError(id, mcpErrTransport, fmt.Sprintf("runtimez API returned HTTP %d", status),
			map[string]any{"status": status}))
	}
}

func (b *mcpBridge) write(body []byte) error {
	body = bytes.TrimRight(body, "\r\n")
	if len(body) == 0 {
		return nil
	}
	if _, err := b.out.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

// injectCluster adds params.arguments.cluster when the call has an arguments object without
// one. Anything malformed is left for the server to reject.
func injectCluster(line []byte, cluster string) ([]byte, bool) {
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, false
	}
	params, ok := msg["params"].(map[string]any)
	if !ok {
		return nil, false
	}
	args, ok := params["arguments"].(map[string]any)
	if !ok {
		if _, present := params["arguments"]; present {
			return nil, false
		}
		args = map[string]any{}
	}
	if v, present := args["cluster"]; present && v != nil && v != "" {
		return nil, false
	}
	args["cluster"] = cluster
	params["arguments"] = args
	msg["params"] = params
	out, err := json.Marshal(msg)
	if err != nil {
		return nil, false
	}
	return out, true
}

func negotiatedVersion(body []byte) string {
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	return resp.Result.ProtocolVersion
}

func jsonRPCError(id json.RawMessage, code int, message string, data any) []byte {
	errObj := map[string]any{"code": code, "message": message}
	if data != nil {
		errObj["data"] = data
	}
	msg := map[string]any{"jsonrpc": "2.0", "error": errObj}
	if len(id) > 0 {
		msg["id"] = id
	} else {
		msg["id"] = nil
	}
	out, _ := json.Marshal(msg)
	return out
}
