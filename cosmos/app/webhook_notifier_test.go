package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
)

// newTestNotifier builds a notifier with tight backoff so retry-path tests
// finish quickly. Callers override url via the returned pointer when needed.
func newTestNotifier(t *testing.T, url string) *webhookNotifier {
	t.Helper()
	n := newWebhookNotifier(url, log.NewNopLogger())
	n.initialBackoff = time.Millisecond
	n.httpClient.Timeout = time.Second
	return n
}

func TestWebhookNotifier_NilReceiverIsNoop(t *testing.T) {
	var n *webhookNotifier
	require.NotPanics(t, func() { n.Notify(upgradeEventName, "hello") })
}

func TestWebhookNotifier_EmptyURLIsNoop(t *testing.T) {
	n := newTestNotifier(t, "")
	require.NotPanics(t, func() { n.Notify(upgradeEventName, "hello") })
}

func TestWebhookNotifier_PostsSlackPayloadOnSuccess(t *testing.T) {
	var calls int32
	var gotBody []byte
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL)
	n.Notify(upgradeEventName, "some alert text")

	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "expected exactly one POST on success")
	require.Equal(t, "application/json", gotContentType)

	var payload slackPayload
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	require.Equal(t, "some alert text", payload.Text)
}

func TestWebhookNotifier_RetriesOn5xxAndSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL)
	n.Notify(haltEventName, "flaky")

	require.Equal(t, int32(2), atomic.LoadInt32(&calls), "should retry once then succeed")
}

func TestWebhookNotifier_GivesUpAfterMaxAttemptsOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL)
	n.Notify(haltEventName, "perma-fail")

	require.Equal(t, int32(webhookDefaultMaxAttempts), atomic.LoadInt32(&calls),
		"should retry up to maxAttempts and then stop")
}

func TestWebhookNotifier_DoesNotRetryOn4xx(t *testing.T) {
	// 4xx means misconfiguration (bad URL, disabled webhook). Retrying wastes
	// time the caller may not have when halt_triggered fires.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL)
	n.Notify(haltEventName, "bad url")

	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "4xx must be terminal")
}

func TestWebhookNotifier_NeverPanicsOnUnreachableURL(t *testing.T) {
	// Port 1 is reserved on most systems — connect will fail quickly.
	n := newTestNotifier(t, "http://127.0.0.1:1/")
	require.NotPanics(t, func() { n.Notify(upgradeEventName, "nobody listening") })
}

func TestFormatUpgradeScheduledText_ContainsContract(t *testing.T) {
	text := formatUpgradeScheduledText(upgradeLog{
		Event:  upgradeEventName,
		Name:   "v2-mainnet",
		Height: 12345678,
		Info:   "https://example.com/release",
	})
	for _, want := range []string{"Upgrade scheduled", "v2-mainnet", "12345678", "https://example.com/release"} {
		require.True(t, strings.Contains(text, want), "expected %q in %q", want, text)
	}
}

func TestFormatHaltTriggeredText_ContainsContract(t *testing.T) {
	text := formatHaltTriggeredText(haltLog{
		Event:     haltEventName,
		PlanName:  "v2-mainnet",
		Height:    12345678,
		Info:      "emergency halt",
		Timestamp: "2026-01-15T12:00:00Z",
	})
	wantFragments := []string{
		"Chain halt triggered", "v2-mainnet", "12345678", "2026-01-15T12:00:00Z", "emergency halt",
	}
	for _, want := range wantFragments {
		require.True(t, strings.Contains(text, want), "expected %q in %q", want, text)
	}
}
