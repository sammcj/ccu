package analysis

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

func TestCreateSessionBlocks(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		entries []models.UsageEntry
		want    int // expected number of blocks
	}{
		{
			name:    "empty entries",
			entries: []models.UsageEntry{},
			want:    0,
		},
		{
			name: "single entry",
			entries: []models.UsageEntry{
				{Timestamp: now, InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4"},
			},
			want: 1,
		},
		{
			name: "entries within same session",
			entries: []models.UsageEntry{
				{Timestamp: now, InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4"},
				{Timestamp: now.Add(1 * time.Hour), InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4"},
			},
			want: 1,
		},
		{
			name: "entries spanning multiple sessions",
			entries: []models.UsageEntry{
				// First entry at 10:30 AM → rounds DOWN to 10:00 AM → session 10 AM - 3 PM (15:00)
				{Timestamp: time.Date(2025, 12, 1, 10, 30, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4"},
				// Second entry at 5:00 PM (17:00) → after first session ends at 3 PM (15:00)
				// No gap block because < 5 hours between sessions (only 2 hours)
				// Rounds to 5:00 PM → session 5 PM - 10 PM
				{Timestamp: time.Date(2025, 12, 1, 17, 0, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4"},
			},
			want: 2, // Two sessions, no gap (gap < 5 hours)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := CreateSessionBlocks(tt.entries)
			if len(blocks) != tt.want {
				t.Errorf("CreateSessionBlocks() got %d blocks, want %d", len(blocks), tt.want)
			}
		})
	}
}

func TestMarkActiveSessions(t *testing.T) {
	now := time.Now()

	blocks := []models.SessionBlock{
		{
			StartTime: now.Add(-2 * time.Hour),
			EndTime:   now.Add(3 * time.Hour),
			IsGap:     false,
		},
		{
			StartTime: now.Add(-10 * time.Hour),
			EndTime:   now.Add(-5 * time.Hour),
			IsGap:     false,
		},
	}

	marked := MarkActiveSessions(blocks, now)

	if !marked[0].IsActive {
		t.Error("First session should be active")
	}

	if marked[1].IsActive {
		t.Error("Second session should not be active")
	}
}

func TestCalculateBurnRate(t *testing.T) {
	now := time.Now()

	// Create a session that's fully within the last hour
	sessionStart := now.Add(-30 * time.Minute)
	blocks := []models.SessionBlock{
		{
			StartTime:    sessionStart,
			EndTime:      sessionStart.Add(5 * time.Hour),
			TotalTokens:  3000, // Total includes cache tokens
			DisplayTokens: 3000, // For this test, display = total
			IsActive:     true,
			IsGap:        false,
			Entries: []models.UsageEntry{
				{Timestamp: sessionStart, InputTokens: 2000, OutputTokens: 1000, Model: "claude-sonnet-4"},
			},
		},
	}

	burnRate := CalculateBurnRate(blocks, now)

	// Expect 3000 tokens in last hour / 60 minutes = 50 tokens/min
	// (The session has been active for 30 min, all tokens are in the last hour)
	expected := 50.0
	if burnRate < expected-1 || burnRate > expected+1 {
		t.Errorf("CalculateBurnRate() = %.2f, want ~%.2f", burnRate, expected)
	}
}

func TestCalculateCacheHitRate(t *testing.T) {
	tests := []struct {
		name        string
		input       int
		cacheCreate int
		cacheRead   int
		want        float64
	}{
		{"all zero", 0, 0, 0, 0},
		{"only fresh input", 1000, 0, 0, 0},
		{"perfect hit rate (cache read only)", 0, 0, 1000, 100},
		{"typical mix", 100, 200, 700, 70},                    // 700 / 1000
		{"50% hit", 100, 100, 200, 50},                        // 200 / 400
		{"only cache writes (no reads)", 100, 900, 0, 0},      // 0 / 1000
		{"large numbers", 532_700, 71_600_000, 1_673_400_000, 95.868}, // approx token-stats example row
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCacheHitRate(tt.input, tt.cacheCreate, tt.cacheRead)
			if got < tt.want-0.01 || got > tt.want+0.01 {
				t.Errorf("CalculateCacheHitRate(%d,%d,%d) = %.4f, want %.4f",
					tt.input, tt.cacheCreate, tt.cacheRead, got, tt.want)
			}
		})
	}
}

func TestCalculateSessionCacheHitRate(t *testing.T) {
	now := time.Now()
	block := &models.SessionBlock{
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now.Add(4 * time.Hour),
		Entries: []models.UsageEntry{
			{InputTokens: 100, CacheCreationTokens: 200, CacheReadTokens: 700},
			{InputTokens: 50, CacheCreationTokens: 50, CacheReadTokens: 400},
		},
	}
	// totals: input=150, cc=250, cr=1100, denom=1500, hit = 73.333%
	got := CalculateSessionCacheHitRate(block)
	want := 73.333
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("CalculateSessionCacheHitRate = %.4f, want %.4f", got, want)
	}

	// Gap block returns 0
	gap := &models.SessionBlock{IsGap: true}
	if rate := CalculateSessionCacheHitRate(gap); rate != 0 {
		t.Errorf("gap block should return 0, got %.4f", rate)
	}

	// Nil returns 0
	if rate := CalculateSessionCacheHitRate(nil); rate != 0 {
		t.Errorf("nil block should return 0, got %.4f", rate)
	}
}

func TestCalculateWindowCacheHitRate(t *testing.T) {
	now := time.Now()

	blocks := []models.SessionBlock{
		// Within last 7 days
		{
			StartTime: now.Add(-2 * 24 * time.Hour),
			Entries: []models.UsageEntry{
				{InputTokens: 100, CacheCreationTokens: 100, CacheReadTokens: 800},
			},
		},
		{
			StartTime: now.Add(-1 * 24 * time.Hour),
			Entries: []models.UsageEntry{
				{InputTokens: 50, CacheCreationTokens: 50, CacheReadTokens: 100},
			},
		},
		// Older than 7 days - excluded
		{
			StartTime: now.Add(-10 * 24 * time.Hour),
			Entries: []models.UsageEntry{
				{InputTokens: 10000, CacheCreationTokens: 0, CacheReadTokens: 0},
			},
		},
		// Gap - skipped
		{
			StartTime: now.Add(-1 * time.Hour),
			IsGap:     true,
			Entries: []models.UsageEntry{
				{InputTokens: 999999},
			},
		},
	}

	got := CalculateWindowCacheHitRate(blocks, now, 7*24*time.Hour)
	// totals: input=150, cc=150, cr=900, denom=1200, hit=75%
	want := 75.0
	if got < want-0.01 || got > want+0.01 {
		t.Errorf("CalculateWindowCacheHitRate = %.4f, want %.4f", got, want)
	}
}

func TestCalculateCostBurnRate(t *testing.T) {
	now := time.Now()
	sessionStart := now.Add(-1 * time.Hour)

	block := models.SessionBlock{
		StartTime: sessionStart,
		EndTime:   sessionStart.Add(5 * time.Hour),
		CostUSD:   6.0, // $6 over 1 hour = $0.10/min
		Entries: []models.UsageEntry{
			{Timestamp: sessionStart, CostUSD: 6.0},
		},
	}

	costBurnRate := CalculateCostBurnRate(block, now)

	expected := 0.10 // $6 / 60 minutes
	if costBurnRate < expected-0.01 || costBurnRate > expected+0.01 {
		t.Errorf("CalculateCostBurnRate() = %.4f, want ~%.4f", costBurnRate, expected)
	}
}

func TestPredictCostDepletion(t *testing.T) {
	now := time.Now()
	costBurnRate := 0.10 // $0.10 per minute
	costRemaining := 5.0 // $5 remaining

	predicted := PredictCostDepletion(costBurnRate, costRemaining, now)

	// Should be 50 minutes from now ($5 / $0.10 per min)
	expected := now.Add(50 * time.Minute)
	diff := predicted.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("PredictCostDepletion() = %v, want ~%v (diff: %v)", predicted, expected, diff)
	}
}

// TestCalculateBurnRate_ProportionalOverlap covers the proportional-overlap
// math that backs the burn-rate prediction (see CLAUDE.md Gotchas). These are
// the cases the documentation calls out specifically: partial overlap with the
// 1-hour window, a completed session entirely inside the window, and sessions
// outside the window.
func TestCalculateBurnRate_ProportionalOverlap(t *testing.T) {
	now := time.Now()

	t.Run("session longer than window contributes proportional slice", func(t *testing.T) {
		// Session ran for 2 hours ending now, 1200 display tokens.
		// 1 hour of the session overlaps the window: 1200 * (60/120) = 600 tokens.
		// 600 tokens / 60 minutes = 10 tokens/min.
		sessionStart := now.Add(-2 * time.Hour)
		end := now
		blocks := []models.SessionBlock{{
			StartTime:     sessionStart,
			EndTime:       sessionStart.Add(5 * time.Hour),
			ActualEndTime: &end,
			DisplayTokens: 1200,
			IsActive:      false,
			IsGap:         false,
		}}

		got := CalculateBurnRate(blocks, now)
		if got < 9.5 || got > 10.5 {
			t.Errorf("proportional overlap burn rate = %.2f, want ~10.0", got)
		}
	})

	t.Run("session shorter than window contributes all its tokens", func(t *testing.T) {
		// 30-minute completed session with 1800 display tokens sits fully inside the window.
		// 1800 tokens / 60 minutes of window = 30 tokens/min.
		sessionStart := now.Add(-45 * time.Minute)
		end := now.Add(-15 * time.Minute)
		blocks := []models.SessionBlock{{
			StartTime:     sessionStart,
			EndTime:       sessionStart.Add(5 * time.Hour),
			ActualEndTime: &end,
			DisplayTokens: 1800,
			IsActive:      false,
			IsGap:         false,
		}}

		got := CalculateBurnRate(blocks, now)
		if got < 29.5 || got > 30.5 {
			t.Errorf("short session burn rate = %.2f, want ~30.0", got)
		}
	})

	t.Run("session that ended before the window is excluded", func(t *testing.T) {
		sessionStart := now.Add(-3 * time.Hour)
		end := now.Add(-90 * time.Minute)
		blocks := []models.SessionBlock{{
			StartTime:     sessionStart,
			EndTime:       sessionStart.Add(5 * time.Hour),
			ActualEndTime: &end,
			DisplayTokens: 10000,
			IsActive:      false,
			IsGap:         false,
		}}

		if got := CalculateBurnRate(blocks, now); got != 0 {
			t.Errorf("out-of-window session burn rate = %.2f, want 0", got)
		}
	})

	t.Run("gap blocks are ignored", func(t *testing.T) {
		sessionStart := now.Add(-30 * time.Minute)
		blocks := []models.SessionBlock{{
			StartTime:     sessionStart,
			EndTime:       sessionStart.Add(2 * time.Hour),
			DisplayTokens: 99999,
			IsGap:         true,
		}}

		if got := CalculateBurnRate(blocks, now); got != 0 {
			t.Errorf("gap-only burn rate = %.2f, want 0", got)
		}
	})

	t.Run("two overlapping sessions both contribute proportionally", func(t *testing.T) {
		// A: 2-hour session ending now, 1200 tokens -> contributes 600 tokens to window.
		// B: 30-minute session inside the window, 900 tokens -> contributes 900 tokens.
		// Total: 1500 tokens in the hour = 25 tokens/min.
		startA := now.Add(-2 * time.Hour)
		endA := now
		startB := now.Add(-40 * time.Minute)
		endB := now.Add(-10 * time.Minute)
		blocks := []models.SessionBlock{
			{
				StartTime:     startA,
				EndTime:       startA.Add(5 * time.Hour),
				ActualEndTime: &endA,
				DisplayTokens: 1200,
			},
			{
				StartTime:     startB,
				EndTime:       startB.Add(5 * time.Hour),
				ActualEndTime: &endB,
				DisplayTokens: 900,
			},
		}

		got := CalculateBurnRate(blocks, now)
		if got < 24.5 || got > 25.5 {
			t.Errorf("two-session burn rate = %.2f, want ~25.0", got)
		}
	})
}

func TestPredictTokenDepletion(t *testing.T) {
	now := time.Now()
	tokenBurnRate := 100.0 // 100 tokens per minute
	tokensRemaining := 5000

	predicted := PredictTokenDepletion(tokenBurnRate, tokensRemaining, now)

	// Should be 50 minutes from now (5000 / 100 per min)
	expected := now.Add(50 * time.Minute)
	diff := predicted.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("PredictTokenDepletion() = %v, want ~%v (diff: %v)", predicted, expected, diff)
	}
}
