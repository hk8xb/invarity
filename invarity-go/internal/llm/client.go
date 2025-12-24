// Package llm provides clients for OpenAI-compatible LLM endpoints.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an OpenAI-compatible API client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	model      string
}

// ClientConfig holds configuration for the LLM client.
type ClientConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	Timeout    time.Duration
}

// NewClient creates a new LLM client.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// ChatMessage represents a message in a chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the request body for chat completions.
type ChatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []ChatMessage `json:"messages"`
	Temperature    float64       `json:"temperature,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Stop           []string      `json:"stop,omitempty"`
}

// ResponseFormat specifies the output format.
type ResponseFormat struct {
	Type   string          `json:"type"` // "json_object" or "text"
	Schema json.RawMessage `json:"schema,omitempty"`
}

// ChatCompletionResponse is the response from chat completions.
type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ChatCompletion sends a chat completion request.
func (c *Client) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// ExtractContent extracts the content from the first choice.
func (r *ChatCompletionResponse) ExtractContent() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

// ExtractJSON extracts and parses JSON from the response content.
func (r *ChatCompletionResponse) ExtractJSON(v any) error {
	content := r.ExtractContent()
	if content == "" {
		return fmt.Errorf("empty response content")
	}
	return json.Unmarshal([]byte(content), v)
}

// MockClient is a mock LLM client for testing.
type MockClient struct {
	responses map[string]*ChatCompletionResponse
	errors    map[string]error
}

// NewMockClient creates a new mock client.
func NewMockClient() *MockClient {
	return &MockClient{
		responses: make(map[string]*ChatCompletionResponse),
		errors:    make(map[string]error),
	}
}

// SetResponse sets a canned response for a given prompt pattern.
func (m *MockClient) SetResponse(pattern string, resp *ChatCompletionResponse) {
	m.responses[pattern] = resp
}

// SetError sets a canned error for a given prompt pattern.
func (m *MockClient) SetError(pattern string, err error) {
	m.errors[pattern] = err
}

// ChatCompletion returns mock responses.
func (m *MockClient) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Check for errors first
	for pattern, err := range m.errors {
		if pattern == "*" {
			return nil, err
		}
	}

	// Return default response
	for pattern, resp := range m.responses {
		if pattern == "*" {
			return resp, nil
		}
	}

	// Default mock response
	return &ChatCompletionResponse{
		ID:      "mock-response",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []struct {
			Index        int         `json:"index"`
			Message      ChatMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		}{
			{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: "{}"},
				FinishReason: "stop",
			},
		},
	}, nil
}
