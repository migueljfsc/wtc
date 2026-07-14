// Package notify posts wtc digests to external sinks. Slack only, for now.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Slack posts text as a Slack incoming-webhook message (mrkdwn). The URL is a
// secret; callers pass it from config/flags, never log it.
func Slack(ctx context.Context, webhookURL, text string) error {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("encode slack message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post to slack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("slack rejected the digest: HTTP %d: %s", resp.StatusCode, snippet)
	}
	return nil
}
