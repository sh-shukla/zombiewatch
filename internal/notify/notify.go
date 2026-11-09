// Package notify delivers zombie reports to a webhook. Delivery failures are
// returned loudly — a notifier that fails silently is itself a zombie.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sh-shukla/zombiewatch/internal/scan"
)

// Payload is the generic JSON body.
type Payload struct {
	Source   string       `json:"source"`
	Watch    string       `json:"watch"`
	ScanTime time.Time    `json:"scanTime"`
	Zombies  []scan.Zombie `json:"zombies"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Send POSTs the report. format "slack" wraps it in a text message;
// anything else sends the structured payload as-is.
func Send(ctx context.Context, url, format string, p Payload) error {
	var body []byte
	var err error
	if format == "slack" {
		body, err = json.Marshal(map[string]string{"text": slackText(p)})
	} else {
		body, err = json.Marshal(p)
	}
	if err != nil {
		return fmt.Errorf("encoding payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting to webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}

const maxSlackLines = 20

func slackText(p Payload) string {
	text := fmt.Sprintf("🧟 *zombiewatch* [%s]: %d zombie(s) found", p.Watch, len(p.Zombies))
	for i, z := range p.Zombies {
		if i == maxSlackLines {
			text += fmt.Sprintf("\n… and %d more", len(p.Zombies)-maxSlackLines)
			break
		}
		text += fmt.Sprintf("\n• `%s` %s %s — %s", z.Check, z.Kind, z.Object, z.Detail)
	}
	return text
}
