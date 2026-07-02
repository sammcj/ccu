package analysis

import (
	"fmt"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/pricing"
)

const (
	// SessionDuration is the 5-hour session window
	SessionDuration = 5 * time.Hour

	// GapThreshold is the threshold for detecting gaps between sessions
	// Must be >= SessionDuration to match Python implementation
	GapThreshold = SessionDuration
)

// CreateSessionBlocks transforms entries into 5-hour session blocks
func CreateSessionBlocks(entries []models.UsageEntry) []models.SessionBlock {
	if len(entries) == 0 {
		return []models.SessionBlock{}
	}

	var blocks []models.SessionBlock
	var currentBlock *models.SessionBlock

	for _, entry := range entries {
		// Price entries that arrive without a cost so PerModelStats.CostUSD is the
		// authoritative per-session cost. entry is a loop-local copy, so the backfill
		// lands in the block via AddEntry without mutating the caller's slice.
		if entry.CostUSD == 0 {
			entry.CostUSD = pricing.CalculateCost(entry)
		}

		if currentBlock == nil {
			// Start first block
			currentBlock = newSessionBlock(entry.Timestamp)
			currentBlock.AddEntry(entry)
		} else {
			// Check if entry fits in current block (before end time)
			if entry.Timestamp.Before(currentBlock.EndTime) {
				// Add to current block
				currentBlock.AddEntry(entry)
			} else {
				// Entry is after current block's end time - finalize and create new block

				// Add gap block if there's a significant gap
				if entry.Timestamp.Sub(currentBlock.EndTime) > GapThreshold {
					gapBlock := createGapBlock(currentBlock.EndTime, entry.Timestamp)
					blocks = append(blocks, gapBlock)
				}

				// Finalize current block
				blocks = append(blocks, *currentBlock)

				// Start new block
				currentBlock = newSessionBlock(entry.Timestamp)
				currentBlock.AddEntry(entry)
			}
		}
	}

	// Add final block if exists
	if currentBlock != nil {
		blocks = append(blocks, *currentBlock)
	}

	return blocks
}

// newSessionBlock creates a new session block starting at the given time
func newSessionBlock(startTime time.Time) *models.SessionBlock {
	// Round DOWN to the hour to match Python implementation
	// Activity at 12:52 PM creates a session starting at noon
	roundedStart := startTime.UTC().Truncate(time.Hour)

	return &models.SessionBlock{
		ID:            fmt.Sprintf("session_%d", roundedStart.Unix()),
		StartTime:     roundedStart,
		EndTime:       roundedStart.Add(SessionDuration),
		PerModelStats: make(map[string]*models.ModelStats),
		IsActive:      false,
		IsGap:         false,
	}
}

// createGapBlock creates a gap block between sessions
func createGapBlock(start, end time.Time) models.SessionBlock {
	return models.SessionBlock{
		ID:        fmt.Sprintf("gap_%d", start.Unix()),
		StartTime: start,
		EndTime:   end,
		IsGap:     true,
		IsActive:  false,
	}
}

// MarkActiveSessions marks sessions as active if they're still within the 5-hour window
func MarkActiveSessions(blocks []models.SessionBlock, now time.Time) []models.SessionBlock {
	for i := range blocks {
		if blocks[i].IsGap {
			continue
		}

		// Session is active if current time is within the session window
		blocks[i].IsActive = now.After(blocks[i].StartTime) && now.Before(blocks[i].EndTime)

		// If session has ended, mark actual end time from the last entry's timestamp
		if !blocks[i].IsActive && blocks[i].ActualEndTime == nil && !blocks[i].LastEntryTime.IsZero() {
			lastEntryTime := blocks[i].LastEntryTime
			blocks[i].ActualEndTime = &lastEntryTime
		}
	}

	return blocks
}

// proportionInLastHour returns the fraction of a session block that falls within
// the hour ending at currentTime. Gap blocks, sessions that have not started yet,
// sessions that ended before the window, and zero-duration sessions all return 0.
// This backs both the token and cost burn-rate calculations - see Appendix B in
// dev plan and CLAUDE.md Gotchas.
func proportionInLastHour(block models.SessionBlock, currentTime time.Time) float64 {
	if block.IsGap {
		return 0
	}

	oneHourAgo := currentTime.Add(-1 * time.Hour)

	// Determine actual end time
	sessionEnd := currentTime
	if !block.IsActive && block.ActualEndTime != nil {
		sessionEnd = *block.ActualEndTime
	} else if !block.IsActive {
		sessionEnd = block.EndTime
	}

	// Check if session overlaps with last hour
	if sessionEnd.Before(oneHourAgo) {
		return 0 // Session ended before the hour window
	}

	if block.StartTime.After(currentTime) {
		return 0 // Session hasn't started yet
	}

	// Calculate overlap period
	sessionStartInHour := block.StartTime
	if oneHourAgo.After(sessionStartInHour) {
		sessionStartInHour = oneHourAgo
	}

	sessionEndInHour := sessionEnd
	if currentTime.Before(sessionEndInHour) {
		sessionEndInHour = currentTime
	}

	if sessionEndInHour.Before(sessionStartInHour) {
		return 0 // No overlap
	}

	// Calculate proportion of session in the hour window
	totalSessionDuration := sessionEnd.Sub(block.StartTime).Minutes()
	if totalSessionDuration <= 0 {
		return 0
	}

	hourDuration := sessionEndInHour.Sub(sessionStartInHour).Minutes()
	return hourDuration / totalSessionDuration
}

// CalculateBurnRate calculates the burn rate using proportional overlapping session calculation
// This is critical - see Appendix B in dev plan
func CalculateBurnRate(blocks []models.SessionBlock, currentTime time.Time) float64 {
	totalTokens := 0.0

	for _, block := range blocks {
		// Use DisplayTokens to match Python (only input + output, no cache tokens)
		totalTokens += float64(block.DisplayTokens) * proportionInLastHour(block, currentTime)
	}

	// Return tokens per minute
	return totalTokens / 60.0
}

// CalculateHourlyCostBurnRate calculates the cost burn rate (USD per minute) across
// all sessions overlapping the last hour, using the same proportional-overlap
// calculation as CalculateBurnRate. Weighting by per-session cost keeps predictions
// accurate under mixed model usage, where per-token cost differs.
func CalculateHourlyCostBurnRate(blocks []models.SessionBlock, currentTime time.Time) float64 {
	totalCost := 0.0

	for _, block := range blocks {
		totalCost += block.CostUSD * proportionInLastHour(block, currentTime)
	}

	// Divide by 60 to get cost per minute (matching token burn rate calculation)
	return totalCost / 60.0
}

// CalculateCostBurnRate calculates the cost burn rate for a session (USD per minute)
func CalculateCostBurnRate(block models.SessionBlock, now time.Time) float64 {
	elapsed := block.ElapsedDuration(now).Minutes()
	if elapsed == 0 {
		return 0
	}

	// Sum the authoritative per-model cost accumulated at entry time.
	totalCost := 0.0
	for _, stats := range block.PerModelStats {
		totalCost += stats.CostUSD
	}

	return totalCost / elapsed
}

// PredictCostDepletion predicts when the cost limit will be reached
// This is the PRIMARY prediction mechanism (see Appendix B)
func PredictCostDepletion(costBurnRate, costRemaining float64, currentTime time.Time) time.Time {
	if costBurnRate <= 0 || costRemaining <= 0 {
		return time.Time{}
	}

	minutesToDepletion := costRemaining / costBurnRate
	return currentTime.Add(time.Duration(minutesToDepletion) * time.Minute)
}

// CalculateCacheHitRate returns the percentage of input tokens served from cache.
// Denominator is fresh input + cache creation + cache read; output tokens are excluded.
// Returns 0 when there is no input activity at all.
func CalculateCacheHitRate(inputTokens, cacheCreationTokens, cacheReadTokens int) float64 {
	denom := inputTokens + cacheCreationTokens + cacheReadTokens
	if denom <= 0 {
		return 0
	}
	return (float64(cacheReadTokens) / float64(denom)) * 100
}

// CalculateSessionCost calculates the total cost for a session
func CalculateSessionCost(block *models.SessionBlock) float64 {
	totalCost := 0.0
	for _, stats := range block.PerModelStats {
		totalCost += stats.CostUSD
	}
	return totalCost
}

// UpdateSessionCosts updates the cost for all sessions
func UpdateSessionCosts(blocks []models.SessionBlock) []models.SessionBlock {
	for i := range blocks {
		if !blocks[i].IsGap {
			blocks[i].CostUSD = CalculateSessionCost(&blocks[i])
			blocks[i].CostBurnRate = CalculateCostBurnRate(blocks[i], time.Now())
		}
	}
	return blocks
}
