package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/lwlee2608/railwaylog/internal/railway"
)

type Client struct {
	http         *http.Client
	auth         *railway.Auth
	httpEndpoint string
	wsEndpoint   string
}

func NewClient(auth *railway.Auth, httpEndpoint, wsEndpoint string) *Client {
	return &Client{
		http:         &http.Client{},
		auth:         auth,
		httpEndpoint: httpEndpoint,
		wsEndpoint:   wsEndpoint,
	}
}

func (c *Client) AuthHeader() (string, string) {
	if c.auth.Kind == railway.TokenProjectAccess {
		return "project-access-token", c.auth.Token
	}
	return "Authorization", "Bearer " + c.auth.Token
}

type gqlRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

func (c *Client) Query(ctx context.Context, query string, variables any, out any) error {
	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.httpEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	name, value := c.AuthHeader()
	req.Header.Set(name, value)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("graphql http %d: %s", resp.StatusCode, truncate(raw, 300))
	}

	var gr gqlResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return fmt.Errorf("decode graphql: %w (body=%s)", err, truncate(raw, 300))
	}
	if len(gr.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", gr.Errors[0].Message)
	}
	if out != nil {
		if err := json.Unmarshal(gr.Data, out); err != nil {
			return fmt.Errorf("decode data: %w", err)
		}
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
