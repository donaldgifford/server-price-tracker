// Package client provides a thin HTTP client for the server-price-tracker API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is a thin HTTP client for the server-price-tracker API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new API client targeting the given base URL.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Option configures the Client.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// get performs a GET request and decodes the JSON response into dst.
func (c *Client) get(ctx context.Context, path string, dst any) error {
	return c.do(ctx, http.MethodGet, path, nil, dst)
}

// post performs a POST request with a JSON body and decodes the response into dst.
func (c *Client) post(ctx context.Context, path string, body, dst any) error {
	return c.do(ctx, http.MethodPost, path, body, dst)
}

// put performs a PUT request with a JSON body and decodes the response into dst.
func (c *Client) put(ctx context.Context, path string, body, dst any) error {
	return c.do(ctx, http.MethodPut, path, body, dst)
}

// del performs a DELETE request and decodes the response into dst.
func (c *Client) del(ctx context.Context, path string, dst any) error {
	return c.do(ctx, http.MethodDelete, path, nil, dst)
}

func (c *Client) do(ctx context.Context, method, path string, body, dst any) error {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return fmt.Errorf("API server not running at %s", c.baseURL)
		}
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	if dst != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dst); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

func isConnectionRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connect: connection refused")
}
