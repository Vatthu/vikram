package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Client manages one MCP server process with concurrent JSON-RPC transport.
type Client struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	writeMu      sync.Mutex
	nextID       atomic.Int64
	timeout      time.Duration
	tools        []ToolDef
	readerCtx    context.Context
	readerCancel context.CancelFunc
	pending      sync.Map // int → chan Response
}

// ClientConfig defines how to spawn an MCP server process.
type ClientConfig struct {
	Command string
	Args    []string
	Timeout time.Duration
}

const maxMCPMessageBytes = 10 * 1024 * 1024

// NewClient spawns the server, initializes, and discovers tools.
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Stderr = nil

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	readerCtx, readerCancel := context.WithCancel(context.Background())

	c := &Client{
		cmd:          cmd,
		stdin:        stdin,
		timeout:      cfg.Timeout,
		readerCtx:    readerCtx,
		readerCancel: readerCancel,
	}
	if c.timeout == 0 {
		c.timeout = 120 * time.Second
	}
	c.nextID.Store(1)

	go c.readLoop(stdoutPipe)

	if _, err := c.call(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "vikram", "version": "1.0"},
	}); err != nil {
		cmd.Process.Kill()
		readerCancel()
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		cmd.Process.Kill()
		readerCancel()
		return nil, fmt.Errorf("mcp initialized notification: %w", err)
	}

	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		cmd.Process.Kill()
		readerCancel()
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}
	var tl ToolListResult
	if err := remarshal(result, &tl); err != nil {
		cmd.Process.Kill()
		readerCancel()
		return nil, fmt.Errorf("mcp parse tools: %w", err)
	}
	c.tools = tl.Tools
	return c, nil
}

func (c *Client) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMCPMessageBytes)
	for scanner.Scan() {
		var resp Response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			continue
		}
		if ch, ok := c.pending.LoadAndDelete(resp.ID); ok {
			select {
			case ch.(chan Response) <- resp:
			default:
			}
		}
	}
}

func (c *Client) notify(ctx context.Context, method string, params interface{}) error {
	req := Request{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, params interface{}) (interface{}, error) {
	id := int(c.nextID.Add(1))
	req := Request{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	respCh := make(chan Response, 1)
	c.pending.Store(id, respCh)
	defer c.pending.Delete(id)

	data, _ := json.Marshal(req)
	data = append(data, '\n')

	c.writeMu.Lock()
	_, err := c.stdin.Write(data)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CallTool sends a tools/call request. Safe for concurrent use.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result, err := c.call(ctx, "tools/call", CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var ct CallToolResult
	if err := remarshal(result, &ct); err != nil {
		return nil, err
	}
	return &ct, nil
}

func (c *Client) Tools() []ToolDef { return c.tools }

func (c *Client) Close() error {
	c.readerCancel()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}

func remarshal(src, dst interface{}) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
