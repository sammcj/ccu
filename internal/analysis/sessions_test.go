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
