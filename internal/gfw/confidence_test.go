package gfw

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// All tests use MemoryConfidenceTracker — the Redis variant shares the same
// logic and is integration-tested against a real Redis instance in CI.

func TestScoreFromState(t *testing.T) {
	tests := []struct {
		name     string
		state    confidenceState
		wantConf float64
	}{
		{
			"zero state — no data",
			confidenceState{},
			0.0,
		},
		{
			"1 count, 1 node → 0.30",
			confidenceState{Count: 1, UniqueNodes: []string{"cn-01"}, LastBlocking: "dns"},
			0.30,
		},
		{
			"2 count, 1 node → 0.50",
			confidenceState{Count: 2, UniqueNodes: []string{"cn-01"}, LastBlocking: "dns"},
			0.50,
		},
		{
			"1 count, 2 nodes → 0.70",
			confidenceState{Count: 1, UniqueNodes: []string{"cn-01", "cn-02"}, LastBlocking: "dns"},
			0.70,
		},
		{
			"2 count, 2 nodes → 0.70",
			confidenceState{Count: 2, UniqueNodes: []string{"cn-01", "cn-02"}, LastBlocking: "dns"},
			0.70,
		},
		{
			"3 count, 2 nodes → 0.90",
			confidenceState{Count: 3, UniqueNodes: []string{"cn-01", "cn-02"}, LastBlocking: "dns"},
			0.90,
		},
		{
			"4 count, 3 nodes → 0.90",
			confidenceState{Count: 4, UniqueNodes: []string{"cn-01", "cn-02", "cn-03"}, LastBlocking: "dns"},
			0.90,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantConf, scoreFromState(tt.state))
		})
	}
}

func TestMemoryTracker_Record_HappyPath(t *testing.T) {
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	// First record → count=1, nodes=1 → 0.30
	conf, err := tr.Record(ctx, 1, "cn-01", "dns")
	require.NoError(t, err)
	assert.Equal(t, 0.30, conf)

	// Second record same node → count=2, nodes=1 → 0.50
	conf, err = tr.Record(ctx, 1, "cn-01", "dns")
	require.NoError(t, err)
	assert.Equal(t, 0.50, conf)

	// Third record second node → count=3, nodes=2 → 0.90 (count≥3 AND nodes≥2)
	conf, err = tr.Record(ctx, 1, "cn-02", "dns")
	require.NoError(t, err)
	assert.Equal(t, 0.90, conf)
}

func TestMemoryTracker_Record_TwoNodesOnFirstRecord(t *testing.T) {
	// Verify 0.70 path: two nodes but count < 3.
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	_, _ = tr.Record(ctx, 5, "cn-01", "dns") // count=1, nodes=1 → 0.30
	conf, err := tr.Record(ctx, 5, "cn-02", "dns") // count=2, nodes=2 → 0.70
	require.NoError(t, err)
	assert.Equal(t, 0.70, conf)
}

func TestMemoryTracker_Record_ResetOnAccessible(t *testing.T) {
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	// Build up state
	_, _ = tr.Record(ctx, 1, "cn-01", "dns")
	_, _ = tr.Record(ctx, 1, "cn-01", "dns")

	// Domain becomes accessible → reset
	conf, err := tr.Record(ctx, 1, "cn-01", "")
	require.NoError(t, err)
	assert.Equal(t, 0.0, conf)

	// Score after reset
	score, err := tr.Score(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0.0, score)
}

func TestMemoryTracker_Record_TypeChangeResetsCount(t *testing.T) {
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	_, _ = tr.Record(ctx, 1, "cn-01", "dns")
	_, _ = tr.Record(ctx, 1, "cn-01", "dns")

	// Blocking type changes → count resets to 1
	conf, err := tr.Record(ctx, 1, "cn-01", "tcp_ip")
	require.NoError(t, err)
	// count=1, nodes still has cn-01 → 0.30
	assert.Equal(t, 0.30, conf)
}

func TestMemoryTracker_Score_NoState(t *testing.T) {
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	score, err := tr.Score(ctx, 999)
	require.NoError(t, err)
	assert.Equal(t, 0.0, score)
}

func TestMemoryTracker_Reset(t *testing.T) {
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	_, _ = tr.Record(ctx, 1, "cn-01", "dns")
	_, _ = tr.Record(ctx, 1, "cn-02", "dns")

	err := tr.Reset(ctx, 1)
	require.NoError(t, err)

	score, _ := tr.Score(ctx, 1)
	assert.Equal(t, 0.0, score)
}

func TestMemoryTracker_Isolation(t *testing.T) {
	// Different domain IDs must not interfere.
	ctx := context.Background()
	tr := NewMemoryConfidenceTracker()

	_, _ = tr.Record(ctx, 1, "cn-01", "dns")
	_, _ = tr.Record(ctx, 2, "cn-01", "dns")
	_, _ = tr.Record(ctx, 2, "cn-01", "dns")

	score1, _ := tr.Score(ctx, 1)
	score2, _ := tr.Score(ctx, 2)

	assert.Equal(t, 0.30, score1)
	assert.Equal(t, 0.50, score2)
}

func TestUniqueStrings(t *testing.T) {
	out := uniqueStrings([]string{"a", "b", "a", "c", "b"})
	assert.Equal(t, []string{"a", "b", "c"}, out)

	assert.Empty(t, uniqueStrings(nil))
	assert.Equal(t, []string{"x"}, uniqueStrings([]string{"x"}))
}
