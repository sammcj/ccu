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
		Entries:       []models.UsageEntry{},
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

		// If session has ended, mark actual end time
		if !blocks[i].IsActive && blocks[i].ActualEndTime == nil && len(blocks[i].Entries) > 0 {
			lastEntry := blocks[i].Entries[len(blocks[i].Entries)-1]
			blocks[i].ActualEndTime = &lastEntry.Timestamp
		}
	}

	return blocks
}

// CalculateBurnRate calculates the burn rate using proportional overlapping session calculation
// This is critical - see Appendix B in dev plan
func CalculateBurnRate(blocks []models.SessionBlock, currentTime time.Time) float64 {
	oneHourAgo := currentTime.Add(-1 * time.Hour)
	totalTokens := 0.0

	for _, block := range blocks {
		if block.IsGap {
			continue
		}

		// Determine actual end time
		sessionEnd := currentTime
		if !block.IsActive && block.ActualEndTime != nil {
			sessionEnd = *block.ActualEndTime
		} else if !block.IsActive {
			sessionEnd = block.EndTime
		}

		// Check if session overlaps with last hour
		if sessionEnd.Before(oneHourAgo) {
			continue // Session ended before the hour window
		}

		if block.StartTime.After(currentTime) {
			continue // Session hasn't started yet
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
			continue // No overlap
		}

		// Calculate proportion of session in the hour window
		totalSessionDuration := sessionEnd.Sub(block.StartTime).Minutes()
		hourDuration := sessionEndInHour.Sub(sessionStartInHour).Minutes()

		if totalSessionDuration > 0 {
			proportion := hourDuration / totalSessionDuration
			// Use DisplayTokens to match Python (only input + output, no cache tokens)
			tokensInHour := float64(block.DisplayTokens) * proportion
			totalTokens += tokensInHour
		}
	}

	// Return tokens per minute
	return totalTokens / 60.0
}

// CalculateCostBurnRate calculates the cost burn rate for a session (USD per minute)
func CalculateCostBurnRate(block models.SessionBlock, now time.Time) float64 {
	elapsed := block.ElapsedDuration(now).Minutes()
	if elapsed == 0 {
		return 0
	}

	// Calculate total cost from entries
	totalCost := 0.0
	for _, entry := range block.Entries {
		if entry.CostUSD > 0 {
			totalCost += entry.CostUSD
		} else {
			totalCost += pricing.CalculateCost(entry)
		}
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

// PredictTokenDepletion predicts when the token limit will be reached
func PredictTokenDepletion(tokenBurnRate float64, tokensRemaining int, currentTime time.Time) time.Time {
	if tokenBurnRate <= 0 || tokensRemaining <= 0 {
		return time.Time{}
	}

	minutesToDepletion := float64(tokensRemaining) / tokenBurnRate
	return currentTime.Add(time.Duration(minutesToDepletion) * time.Minute)
}

// CalculateSessionCost calculates the total cost for a session
func CalculateSessionCost(block *models.SessionBlock) float64 {
	totalCost := 0.0
	for _, entry := range block.Entries {
		if entry.CostUSD > 0 {
			totalCost += entry.CostUSD
		} else {
			totalCost += pricing.CalculateCost(entry)
		}
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

// GetActiveSession returns the currently active session, if any
func GetActiveSession(blocks []models.SessionBlock) *models.SessionBlock {
	for i := range blocks {
		if blocks[i].IsActive && !blocks[i].IsGap {
			return &blocks[i]
		}
	}
	return nil
}

// GetMostRecentSession returns the most recent non-gap session
func GetMostRecentSession(blocks []models.SessionBlock) *models.SessionBlock {
	for i := len(blocks) - 1; i >= 0; i-- {
		if !blocks[i].IsGap {
			return &blocks[i]
		}
	}
	return nil
}

// IsSessionLimitCritical returns true if any session limit (tokens, cost, or messages) is >95%
func IsSessionLimitCritical(session *models.SessionBlock, limits models.Limits) bool {
	if session == nil {
		return false
	}

	var tokenPercent, costPercent, messagePercent float64

	if limits.TokenLimit > 0 {
		tokenPercent = float64(session.DisplayTokens) / float64(limits.TokenLimit) * 100
	}
	if limits.CostLimitUSD > 0 {
		costPercent = session.CostUSD / limits.CostLimitUSD * 100
	}
	if limits.MessageLimit > 0 {
		messagePercent = float64(session.MessageCount) / float64(limits.MessageLimit) * 100
	}

	return tokenPercent > 95 || costPercent > 95 || messagePercent > 95
}

// IsSessionLimitApproaching returns true if any session limit is >80% but <=95%
func IsSessionLimitApproaching(session *models.SessionBlock, limits models.Limits) bool {
	if session == nil {
		return false
	}

	var tokenPercent, costPercent, messagePercent float64

	if limits.TokenLimit > 0 {
		tokenPercent = float64(session.DisplayTokens) / float64(limits.TokenLimit) * 100
	}
	if limits.CostLimitUSD > 0 {
		costPercent = session.CostUSD / limits.CostLimitUSD * 100
	}
	if limits.MessageLimit > 0 {
		messagePercent = float64(session.MessageCount) / float64(limits.MessageLimit) * 100
	}

	maxPercent := tokenPercent
	if costPercent > maxPercent {
		maxPercent = costPercent
	}
	if messagePercent > maxPercent {
		maxPercent = messagePercent
	}

	return maxPercent > 80 && maxPercent <= 95
}

// GetSessionLimitStatus returns the highest limit percentage and which limit it is
func GetSessionLimitStatus(session *models.SessionBlock, limits models.Limits) (percent float64, limitType string) {
	if session == nil {
		return 0, ""
	}

	var tokenPercent, costPercent, messagePercent float64

	if limits.TokenLimit > 0 {
		tokenPercent = float64(session.DisplayTokens) / float64(limits.TokenLimit) * 100
	}
	if limits.CostLimitUSD > 0 {
		costPercent = session.CostUSD / limits.CostLimitUSD * 100
	}
	if limits.MessageLimit > 0 {
		messagePercent = float64(session.MessageCount) / float64(limits.MessageLimit) * 100
	}

	// Find the highest percentage among active limits
	maxPercent := 0.0
	limitType = ""

	// Only consider token limit if it's configured (>0)
	if limits.TokenLimit > 0 && tokenPercent > maxPercent {
		maxPercent = tokenPercent
		limitType = "tokens"
	}
	if costPercent > maxPercent {
		maxPercent = costPercent
		limitType = "cost"
	}
	if messagePercent > maxPercent {
		maxPercent = messagePercent
		limitType = "messages"
	}

	return maxPercent, limitType
}
