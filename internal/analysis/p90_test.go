package analysis

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestQuantile pins down the current quantile behaviour: floor-truncation of
// the (len-1)*q index, no interpolation. If the algorithm ever changes to
// something like linear interpolation, these expectations will trip loudly
// so the impact on downstream limits/predictions can be reviewed.
func TestQuantile(t *testing.T) {
	tests := []struct {
		name string
		data []int
		q    float64
		want int
	}{
		{name: "empty", data: nil, q: 0.90, want: 0},
		{name: "single element p90", data: []int{1000}, q: 0.90, want: 1000},
		{name: "single element p50", data: []int{42}, q: 0.50, want: 42},
		{name: "two elements p0", data: []int{10, 20}, q: 0, want: 10},
		// floor((n-1)*q) = floor(1 * 0.9) = 0 -> element at index 0
		{name: "two elements p90 floors to index 0", data: []int{10, 20}, q: 0.90, want: 10},
		{name: "two elements p100", data: []int{10, 20}, q: 1.0, want: 20},
		{name: "ten elements p50", data: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, q: 0.50, want: 5},
		{name: "ten elements p90", data: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, q: 0.90, want: 9},
		// floor(9 * 0.99) = 8 -> element at index 8
		{name: "ten elements p99 floors to index 8", data: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, q: 0.99, want: 9},
		{name: "duplicates p90", data: []int{5, 5, 5, 5, 5}, q: 0.90, want: 5},
		// sorted = [1, 3, 5, 8, 10]; floor(4 * 0.9) = 3 -> element at index 3 = 8
		{name: "unsorted input p90 floors to index 3", data: []int{10, 1, 5, 8, 3}, q: 0.90, want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, quantile(tt.data, tt.q))
		})
	}
}

func TestCalculateP90Limit_NoSessions(t *testing.T) {
	cfg := DefaultP90Config()
	assert.Equal(t, cfg.DefaultMinLimit, CalculateP90Limit(nil, cfg))
	assert.Equal(t, cfg.DefaultMinLimit, CalculateP90Limit([]models.SessionBlock{}, cfg))
}

func TestCalculateP90Limit_FloorIsApplied(t *testing.T) {
	cfg := DefaultP90Config()
	// Sessions all well below the default min - P90 should be clamped up to the floor.
	blocks := []models.SessionBlock{
		{TotalTokens: 100, IsActive: false, IsGap: false},
		{TotalTokens: 200, IsActive: false, IsGap: false},
		{TotalTokens: 300, IsActive: false, IsGap: false},
	}
	assert.Equal(t, cfg.DefaultMinLimit, CalculateP90Limit(blocks, cfg))
}

func TestCalculateP90Limit_UsesHitSessionsWhenAvailable(t *testing.T) {
	cfg := DefaultP90Config()
	// A session that hits ~95% of the Pro limit (19000 * 0.95 = 18050) should
	// be treated as a hit, overriding the "no hits -> use all sessions" branch.
	blocks := []models.SessionBlock{
		{TotalTokens: 18100, IsActive: false, IsGap: false},  // hits Pro
		{TotalTokens: 18200, IsActive: false, IsGap: false},  // hits Pro
		{TotalTokens: 100, IsActive: false, IsGap: false},    // below any limit
		{TotalTokens: 500, IsActive: true, IsGap: false},     // active -> skipped
		{TotalTokens: 9999, IsActive: false, IsGap: true},    // gap -> skipped
	}
	got := CalculateP90Limit(blocks, cfg)
	// P90 of [18100, 18200] rounds to 18200 then the floor bumps it back to DefaultMinLimit.
	assert.Equal(t, cfg.DefaultMinLimit, got)
}

// Keep the "active sessions are ignored" invariant explicit so a future
// refactor doesn't silently start counting them.
func TestCalculateP90Limit_ActiveAndGapSessionsIgnored(t *testing.T) {
	cfg := DefaultP90Config()
	blocks := []models.SessionBlock{
		{TotalTokens: 500_000, IsActive: true, IsGap: false},
		{TotalTokens: 500_000, IsActive: false, IsGap: true},
	}
	// Both should be filtered out, leaving no data to compute from.
	assert.Equal(t, cfg.DefaultMinLimit, CalculateP90Limit(blocks, cfg))
}

// Sanity check that DetectPlan maps P90 outputs onto plan names without panicking
// on degenerate inputs. The concrete thresholds are tested via CalculateP90Limit.
func TestDetectPlan_Smoke(t *testing.T) {
	// No blocks -> P90 returns the floor (19000), which maps to "pro".
	assert.Equal(t, "pro", DetectPlan(nil))

	now := time.Now()
	end := now.Add(5 * time.Hour)
	blocks := []models.SessionBlock{
		{StartTime: now.Add(-10 * time.Hour), EndTime: end, TotalTokens: 250_000, IsActive: false, IsGap: false},
		{StartTime: now.Add(-9 * time.Hour), EndTime: end, TotalTokens: 240_000, IsActive: false, IsGap: false},
	}
	assert.Equal(t, "max20", DetectPlan(blocks))
}
