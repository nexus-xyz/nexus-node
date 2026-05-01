package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"cosmossdk.io/log"
)

// WebhookURLEnvVar is the environment variable that configures the Slack
// incoming-webhook URL for upgrade_scheduled and halt_triggered alerts. When
// unset, the notifier is a no-op and only the structured log emission from
// ENG-1446 / ENG-1445 remains in effect.
const WebhookURLEnvVar = "NEXUS_ALERT_WEBHOOK_URL"

const (
	webhookDefaultHTTPTimeout    = 5 * time.Second
	webhookDefaultMaxAttempts    = 3
	webhookDefaultInitialBackoff = 1 * time.Second
)

// webhookNotifier posts Slack-formatted messages to an incoming-webhook URL
// for the upgrade_scheduled and halt_triggered events defined in
// UPGRADE_ALERTING.md. This is the direct-notification path (ENG-1491);
// log-scrape alerting via Vector/Loki remains available in parallel.
//
// The notifier is deliberately minimal: Slack-shaped payload (`{"text": ...}`)
// and a single URL from an env var. Multi-sink and generic payload shapes are
// planned follow-ups.
//
// A nil receiver or empty URL is a silent no-op — callers always invoke.
// Failures are logged only; they must never halt block production or panic.
type webhookNotifier struct {
	url            string
	httpClient     *http.Client
	maxAttempts    int
	initialBackoff time.Duration
	logger         log.Logger
}

func newWebhookNotifierFromEnv(logger log.Logger) *webhookNotifier {
	return newWebhookNotifier(os.Getenv(WebhookURLEnvVar), logger)
}

func newWebhookNotifier(url string, logger log.Logger) *webhookNotifier {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &webhookNotifier{
		url:            url,
		httpClient:     &http.Client{Timeout: webhookDefaultHTTPTimeout},
		maxAttempts:    webhookDefaultMaxAttempts,
		initialBackoff: webhookDefaultInitialBackoff,
		logger:         logger,
	}
}

// slackPayload is the body shape accepted by a Slack incoming webhook. `text`
// supports Slack mrkdwn — asterisks for bold, backticks for code.
type slackPayload struct {
	Text string `json:"text"`
}

// Notify posts a Slack message for event. Blocks until the request succeeds,
// exhausts retries, or the internal deadline elapses. Safe to call on a nil
// receiver or when the URL is unset.
//
// Retries use exponential backoff on network errors and 5xx responses. 4xx
// responses are treated as terminal — Slack returning 4xx almost always means
// a permanent config problem (bad URL, disabled webhook) and retrying wastes
// time the caller may not have (halt_triggered fires moments before the halt).
func (n *webhookNotifier) Notify(event, text string) {
	if n == nil || n.url == "" {
		return
	}

	body, err := json.Marshal(slackPayload{Text: text})
	if err != nil {
		n.logger.Error("webhook notifier: marshal failed", "event", event, "error", err)
		return
	}

	backoff := n.initialBackoff
	var lastErr error
	for attempt := 1; attempt <= n.maxAttempts; attempt++ {
		retryable, err := n.postOnce(body)
		if err == nil {
			return
		}
		lastErr = err
		if !retryable || attempt == n.maxAttempts {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	n.logger.Error(
		"webhook notifier: delivery failed",
		"event", event,
		"attempts", n.maxAttempts,
		"error", lastErr,
	)
}

// postOnce performs a single POST. The bool is true when the error is worth
// retrying (network failures, 5xx) and false for terminal conditions (4xx,
// bad request construction).
func (n *webhookNotifier) postOnce(body []byte) (retryable bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), webhookDefaultHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	if resp.StatusCode >= 500 {
		return true, fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return false, fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

// formatUpgradeScheduledText renders an upgrade_scheduled payload as Slack
// mrkdwn. Keep in sync with the log contract in UPGRADE_ALERTING.md.
func formatUpgradeScheduledText(p upgradeLog) string {
	return fmt.Sprintf(
		":warning: *Upgrade scheduled* — plan `%s` will halt the chain at height `%d`.\nInfo: %s",
		p.Name, p.Height, p.Info,
	)
}

// formatHaltTriggeredText renders a halt_triggered payload as Slack mrkdwn.
func formatHaltTriggeredText(p haltLog) string {
	return fmt.Sprintf(
		":rotating_light: *Chain halt triggered* — plan `%s` at height `%d` (%s).\nInfo: %s",
		p.PlanName, p.Height, p.Timestamp, p.Info,
	)
}
