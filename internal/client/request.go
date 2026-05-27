package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is an authenticated HTTP client for the Artifactory REST API.
type Client struct {
	http             *http.Client
	artifactoryURL   *url.URL
	artifactoryToken string
	verbose          bool
}

// NewClient creates a Client from the given Config.
func NewClient(config Config) (*Client, error) {
	c := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 10 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConnsPerHost: 128,
			IdleConnTimeout:     60 * time.Second,
		},
	}

	parsedURL, err := url.Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory URL %q: %w", config.URL, err)
	}

	return &Client{
		http:             c,
		artifactoryURL:   parsedURL,
		artifactoryToken: config.Token,
		verbose:          config.Verbose,
	}, nil
}

// isLegacyAPIKey returns true when the token is an old-style Artifactory API key
// (starts with "AKC"). Everything else — JWT and Reference Tokens — uses Bearer.
func isLegacyAPIKey(token string) bool {
	return strings.HasPrefix(token, "AKC")
}

// httpError formats a non-2xx response as an error with the status and body.
func httpError(status int, body []byte) error {
	return fmt.Errorf("request failed: HTTP %d: %s", status, body)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, extraHeaders map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.artifactoryURL.String()+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	c.setAuthHeader(req)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "text/plain")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	c.logRequest(req, body)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	c.logResponse(resp, respBody)

	return respBody, resp.StatusCode, nil
}

func (c *Client) setAuthHeader(req *http.Request) {
	if isLegacyAPIKey(c.artifactoryToken) {
		req.Header.Set("X-JFrog-Art-Api", c.artifactoryToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.artifactoryToken)
	}
}

func (c *Client) logRequest(req *http.Request, body []byte) {
	if !c.verbose {
		return
	}
	fmt.Printf(">>> %s %s\n", req.Method, req.URL)
	for k, v := range req.Header {
		if kl := strings.ToLower(k); kl == "x-jfrog-art-api" || kl == "authorization" {
			fmt.Printf("    %s: [redacted]\n", k)
		} else {
			fmt.Printf("    %s: %s\n", k, v)
		}
	}
	if len(body) > 0 {
		fmt.Printf("    body: %s\n", truncate(string(body), 500))
	}
}

func (c *Client) logResponse(resp *http.Response, body []byte) {
	if !c.verbose {
		return
	}
	fmt.Printf("<<< %d %s\n", resp.StatusCode, resp.Request.URL.Path)
	fmt.Printf("    body: %s\n\n", truncate(string(body), 500))
}

// Post sends an AQL query (POST with text body) and returns the response body.
func (c *Client) Post(ctx context.Context, path, payload string) ([]byte, error) {
	body, status, err := c.do(ctx, "POST", path, []byte(payload), nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, httpError(status, body)
	}
	if string(body) == "null" {
		return nil, nil
	}
	return body, nil
}

// PostJSON sends a POST with application/json body.
func (c *Client) PostJSON(ctx context.Context, path, payload string) ([]byte, error) {
	body, status, err := c.do(ctx, "POST", path, []byte(payload),
		map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, httpError(status, body)
	}
	return body, nil
}

// Get fetches the content at path and returns the response body.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	body, status, err := c.do(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, httpError(status, body)
	}
	return body, nil
}

// GetOptional fetches the content at path. Returns (nil, nil) on 404.
func (c *Client) GetOptional(ctx context.Context, path string) ([]byte, error) {
	body, status, err := c.do(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	if status >= 400 {
		return nil, httpError(status, body)
	}
	return body, nil
}

// GetWithHeaders fetches path with additional request headers. Returns (nil, nil) on 404.
func (c *Client) GetWithHeaders(ctx context.Context, path string, headers map[string]string) ([]byte, error) {
	body, status, err := c.do(ctx, "GET", path, nil, headers)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	if status >= 400 {
		return nil, httpError(status, body)
	}
	return body, nil
}

// Delete sends a DELETE request to path.
func (c *Client) Delete(ctx context.Context, path string) error {
	body, status, err := c.do(ctx, "DELETE", path, nil, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("delete failed: HTTP %d: %s", status, body)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... [%d bytes total]", len(s))
}
