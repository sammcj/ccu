package analysis

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestRealDataPattern(t *testing.T) {
	// Simulate your actual data pattern from today
	entries := []models.UsageEntry{
		// Morning activity: 10:08 AM - 11:37 AM local (Nov 30 23:08 UTC - Dec 1 00:37 UTC)
		{Timestamp: time.Date(2024, 11, 30, 23, 8, 39, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "sonnet"},
		{Timestamp: time.Date(2025, 12, 1, 0, 37, 30, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "sonnet"},

		// 74-minute GAP

		// Afternoon activity resumes: 12:52 PM local (01:52 UTC)
		{Timestamp: time.Date(2025, 12, 1, 1, 52, 20, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "sonnet"},
		{Timestamp: time.Date(2025, 12, 1, 2, 0, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "sonnet"},

		// More activity continuing through afternoon
		{Timestamp: time.Date(2025, 12, 1, 4, 0, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "sonnet"},
		{Timestamp: time.Date(2025, 12, 1, 5, 0, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, Model: "sonnet"},
	}

	blocks := CreateSessionBlocks(entries)
	now := time.Date(2025, 12, 1, 5, 47, 0, 0, time.UTC) // 4:47 PM local
	blocks = MarkActiveSessions(blocks, now)

	// Print what we got
	t.Logf("Created %d blocks:", len(blocks))
	for i, block := range blocks {
		if block.IsGap {
			t.Logf("  Block %d: GAP from %s to %s", i+1,
				block.StartTime.Format("15:04"),
				block.EndTime.Format("15:04"))
		} else {
			t.Logf("  Block %d: SESSION from %s to %s (UTC), Active=%v, Entries=%d",
				i+1,
				block.StartTime.Format("15:04"),
				block.EndTime.Format("15:04"),
				block.IsActive,
				len(block.Entries))
		}
	}

	// Sessions end 5 hours after their start time
	// Last entry is at 05:00 UTC, so session starts at 05:00 UTC (rounded down)
	// and ends at 10:00 UTC (05:00 + 5 hours)

	// Find active session
	var activeSession *models.SessionBlock
	for i := range blocks {
		if blocks[i].IsActive && !blocks[i].IsGap {
			activeSession = &blocks[i]
			break
		}
	}

	assert.NotNil(t, activeSession, "Should have an active session")
	if activeSession != nil {
		t.Logf("\nActive session: %s to %s UTC",
			activeSession.StartTime.Format("15:04"),
			activeSession.EndTime.Format("15:04"))

		// The active session should start at 05:00 UTC and end at 10:00 UTC (start + 5 hours)
		expectedStart := time.Date(2025, 12, 1, 5, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)
		assert.Equal(t, expectedStart, activeSession.StartTime, "Active session should start at 05:00 UTC")
		assert.Equal(t, expectedEnd, activeSession.EndTime, "Active session should end at 10:00 UTC (start + 5 hours)")
	}
}
