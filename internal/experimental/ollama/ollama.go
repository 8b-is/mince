// Package ollama is a small NDJSON client for the local Ollama daemon. It
// pins keep_alive=-1 and think=false so mince never unloads a model between
// turns or spends latency on hidden reasoning tokens.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to Ollama.
type Client struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

// New returns a client with a 120s timeout.
func New(baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Model: model, HTTP: &http.Client{Timeout: 120 * time.Second}}
}

type chatRequest struct {
	Model     string          `json:"model"`
	Messages  []Message       `json:"messages"`
	Stream    bool            `json:"stream"`
	Format    json.RawMessage `json:"format,omitempty"`
	KeepAlive int             `json:"keep_alive"`
	Think     bool            `json:"think"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error"`
}

// Chat sends messages and returns the assistant text. schema may be nil; when
// set it is passed as Ollama's JSON-schema `format`.
func (c *Client) Chat(ctx context.Context, messages []Message, schema json.RawMessage) (string, error) {
	if c == nil || c.Model == "" {
		return "", fmt.Errorf("ollama: no model configured")
	}
	reqBody := chatRequest{
		Model:     c.Model,
		Messages:  messages,
		Stream:    false,
		Format:    schema,
		KeepAlive: -1,
		Think:     false,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ollama: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var cr chatResponse
		if err := json.Unmarshal(line, &cr); err != nil {
			return "", fmt.Errorf("ollama: bad ndjson: %w", err)
		}
		if cr.Error != "" {
			return "", fmt.Errorf("ollama: %s", cr.Error)
		}
		out.WriteString(cr.Message.Content)
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ChatInto asks for schema-constrained JSON and decodes it into out.
func (c *Client) ChatInto(ctx context.Context, messages []Message, schema json.RawMessage, out any) error {
	text, err := c.Chat(ctx, messages, schema)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(text), out)
}
