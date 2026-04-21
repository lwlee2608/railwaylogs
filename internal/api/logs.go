package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"

	"github.com/lwlee2608/railwaylog/internal/output"
)

const deploymentLogsSubscription = `subscription DeploymentLogs($deploymentId: String!, $filter: String, $limit: Int) {
  deploymentLogs(deploymentId: $deploymentId, filter: $filter, limit: $limit) {
    timestamp
    message
    attributes { key value }
  }
}`

// streamState tracks dedupe state across reconnects.
type streamState struct {
	lastTimestamp string
}

// wsMessage is a graphql-transport-ws envelope.
type wsMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type subscribePayload struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type nextPayload struct {
	Data struct {
		DeploymentLogs []struct {
			Timestamp  string `json:"timestamp"`
			Message    string `json:"message"`
			Attributes []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"attributes"`
		} `json:"deploymentLogs"`
	} `json:"data"`
}

// StreamDeployLogs subscribes to deploymentLogs and writes each line to out,
// reconnecting on error until ctx is done or retries are exhausted.
func (c *Client) StreamDeployLogs(ctx context.Context, deploymentID string, out *output.Writer, retry RetryConfig) error {
	state := &streamState{}
	backoff := NewBackoff(retry)

	for {
		received, err := c.runStream(ctx, deploymentID, state, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if received {
			backoff.Reset()
		}

		delay, ok := backoff.Next()
		if !ok {
			return fmt.Errorf("log stream: %w (retries exhausted)", err)
		}

		fmt.Fprintf(os.Stderr, "railwaylog: stream closed (%v); reconnecting in %s\n", err, delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// runStream runs a single connection; returns whether any logs were received.
func (c *Client) runStream(ctx context.Context, deploymentID string, state *streamState, out *output.Writer) (bool, error) {
	headerName, headerValue := c.AuthHeader()
	header := http.Header{}
	header.Set(headerName, headerValue)

	conn, _, err := websocket.Dial(ctx, c.wsEndpoint, &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: []string{"graphql-transport-ws"},
	})
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()

	if err := writeJSON(ctx, conn, wsMessage{Type: "connection_init", Payload: json.RawMessage("{}")}); err != nil {
		return false, fmt.Errorf("connection_init: %w", err)
	}

	ack, err := readMessage(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("read ack: %w", err)
	}
	if ack.Type != "connection_ack" {
		return false, fmt.Errorf("expected connection_ack, got %s: %s", ack.Type, ack.Payload)
	}

	subPayload, err := json.Marshal(subscribePayload{
		Query: deploymentLogsSubscription,
		Variables: map[string]any{
			"deploymentId": deploymentID,
			"filter":       "",
			"limit":        500,
		},
	})
	if err != nil {
		return false, err
	}

	if err := writeJSON(ctx, conn, wsMessage{ID: "1", Type: "subscribe", Payload: subPayload}); err != nil {
		return false, fmt.Errorf("subscribe: %w", err)
	}

	received := false
	for {
		msg, err := readMessage(ctx, conn)
		if err != nil {
			return received, err
		}

		switch msg.Type {
		case "next":
			var p nextPayload
			if err := json.Unmarshal(msg.Payload, &p); err != nil {
				return received, fmt.Errorf("decode next: %w", err)
			}
			for _, line := range p.Data.DeploymentLogs {
				if state.lastTimestamp != "" && line.Timestamp <= state.lastTimestamp {
					continue
				}
				state.lastTimestamp = line.Timestamp
				received = true

				ol := output.LogLine{
					Timestamp: line.Timestamp,
					Message:   line.Message,
				}
				for _, a := range line.Attributes {
					ol.Attributes = append(ol.Attributes, output.Attribute{Key: a.Key, Value: a.Value})
				}
				if err := out.Write(ol); err != nil {
					return received, fmt.Errorf("write: %w", err)
				}
			}
		case "ping":
			if err := writeJSON(ctx, conn, wsMessage{Type: "pong"}); err != nil {
				return received, fmt.Errorf("pong: %w", err)
			}
		case "error":
			return received, fmt.Errorf("server error: %s", msg.Payload)
		case "complete":
			conn.Close(websocket.StatusNormalClosure, "")
			return received, fmt.Errorf("server closed stream")
		}
	}
}

func writeJSON(ctx context.Context, conn *websocket.Conn, msg wsMessage) error {
	buf, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, buf)
}

func readMessage(ctx context.Context, conn *websocket.Conn) (*wsMessage, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var msg wsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("decode ws message: %w", err)
	}
	return &msg, nil
}
