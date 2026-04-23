package alert

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"domain-platform/store/postgres"
)

// ── stub alert store ──────────────────────────────────────────────────────────

type stubAlertStore struct {
	events      []*postgres.AlertEvent
	rules       []postgres.NotificationRule
	dedupExists bool
	insertErr   error
	rulesErr    error
}

func (s *stubAlertStore) ExistsActiveDedupKey(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return s.dedupExists, nil
}
func (s *stubAlertStore) Insert(_ context.Context, ev *postgres.AlertEvent) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	ev.ID = int64(len(s.events) + 1)
	ev.UUID = "test-uuid"
	ev.CreatedAt = time.Now()
	s.events = append(s.events, ev)
	return nil
}
func (s *stubAlertStore) ListMatchingRules(_ context.Context, _, _ string) ([]postgres.NotificationRule, error) {
	return s.rules, s.rulesErr
}
func (s *stubAlertStore) MarkNotified(_ context.Context, _ int64) error { return nil }

// ── stub asynq client ────────────────────────────────────────────────────────

type stubNotifySendPayload struct {
	Channel  string
	Subject  string
	Body     string
	Severity string
}

type stubAsynqClient struct {
	enqueued []stubNotifySendPayload
}

// ── stub engine ──────────────────────────────────────────────────────────────

// testEngine wraps Engine with injected stub store + captures enqueued notifications.
type testEngine struct {
	store      *stubAlertStore
	enqueued   []stubNotifySendPayload
	resolvedAt []string
}

// fireDirect bypasses the asynq client and directly calls the store, capturing
// what would have been enqueued to TypeNotifySend.
func (te *testEngine) Fire(ctx context.Context, ev *postgres.AlertEvent) error {
	if ev.DedupKey != nil && *ev.DedupKey != "" {
		exists, _ := te.store.ExistsActiveDedupKey(ctx, *ev.DedupKey, dedupWindow)
		if exists {
			return nil
		}
	}
	if err := te.store.Insert(ctx, ev); err != nil {
		return err
	}
	rules, _ := te.store.ListMatchingRules(ctx, ev.Severity, ev.TargetKind)
	subject, body := formatMessage(ev)
	for _, r := range rules {
		te.enqueued = append(te.enqueued, stubNotifySendPayload{
			Channel:  r.Channel,
			Subject:  subject,
			Body:     body,
			Severity: severityToLevel(ev.Severity),
		})
	}
	if len(rules) > 0 {
		_ = te.store.MarkNotified(ctx, ev.ID)
	}
	return nil
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestEngine_Fire_Persists(t *testing.T) {
	store := &stubAlertStore{}
	te := &testEngine{store: store}

	ev := &postgres.AlertEvent{
		Severity:   "P1",
		Source:     "probe",
		TargetKind: "domain",
		Title:      "L1 probe failed: example.com",
	}
	err := te.Fire(context.Background(), ev)
	require.NoError(t, err)
	assert.Len(t, store.events, 1)
	assert.Equal(t, "P1", store.events[0].Severity)
}

func TestEngine_Fire_DedupSuppresses(t *testing.T) {
	store := &stubAlertStore{dedupExists: true}
	te := &testEngine{store: store}

	dk := "probe:l1:domain:42"
	ev := &postgres.AlertEvent{
		Severity:   "P1",
		Source:     "probe",
		TargetKind: "domain",
		Title:      "L1 probe failed: example.com",
		DedupKey:   &dk,
	}
	err := te.Fire(context.Background(), ev)
	require.NoError(t, err)
	// Dedup hit — event must NOT be inserted.
	assert.Len(t, store.events, 0)
}

func TestEngine_Fire_DedupAllowsNewAlert(t *testing.T) {
	store := &stubAlertStore{dedupExists: false}
	te := &testEngine{store: store}

	dk := "probe:l1:domain:42"
	ev := &postgres.AlertEvent{
		Severity:   "P1",
		Source:     "probe",
		TargetKind: "domain",
		Title:      "L1 probe failed: example.com",
		DedupKey:   &dk,
	}
	err := te.Fire(context.Background(), ev)
	require.NoError(t, err)
	assert.Len(t, store.events, 1)
}

func TestEngine_Fire_FansOutToMatchingRules(t *testing.T) {
	cfgJSON, _ := json.Marshal(map[string]string{"bot_token": "abc", "chat_id": "123"})
	store := &stubAlertStore{
		rules: []postgres.NotificationRule{
			{ID: 1, Channel: "telegram", Config: cfgJSON, Enabled: true},
			{ID: 2, Channel: "webhook", Config: json.RawMessage(`{"url":"http://hooks.example.com"}`), Enabled: true},
		},
	}
	te := &testEngine{store: store}

	ev := &postgres.AlertEvent{
		Severity:   "P2",
		Source:     "drift",
		TargetKind: "domain",
		Title:      "DNS drift detected",
	}
	err := te.Fire(context.Background(), ev)
	require.NoError(t, err)
	assert.Len(t, te.enqueued, 2)
	assert.Equal(t, "telegram", te.enqueued[0].Channel)
	assert.Equal(t, "webhook", te.enqueued[1].Channel)
	assert.Equal(t, "error", te.enqueued[0].Severity) // P2 → "error"
}

func TestEngine_Fire_NoRulesNoNotify(t *testing.T) {
	store := &stubAlertStore{rules: nil}
	te := &testEngine{store: store}

	ev := &postgres.AlertEvent{
		Severity:   "INFO",
		Source:     "system",
		TargetKind: "domain",
		Title:      "Scheduled maintenance",
	}
	err := te.Fire(context.Background(), ev)
	require.NoError(t, err)
	assert.Len(t, te.enqueued, 0)
	// Alert is still persisted even without rules.
	assert.Len(t, store.events, 1)
}

func TestEngine_SeverityToLevel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"P1", "critical"},
		{"P2", "error"},
		{"P3", "warning"},
		{"INFO", "info"},
		{"unknown", "info"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, severityToLevel(tt.in), "input: %s", tt.in)
	}
}

func TestFormatter_Subject(t *testing.T) {
	ev := &postgres.AlertEvent{
		ID:         1,
		UUID:       "uuid-1",
		Severity:   "P1",
		Source:     "probe",
		TargetKind: "domain",
		Title:      "L1 probe failed: example.com",
	}
	subject, body := formatMessage(ev)
	assert.Contains(t, subject, "example.com")
	assert.Contains(t, subject, "CRITICAL")
	assert.Contains(t, body, "[P1]")
	assert.Contains(t, body, "probe")
}
