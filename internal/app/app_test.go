package app

import (
	"errors"
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
)

// applyMsg delivers a message through Update and returns the resulting model value
func applyMsg(t *testing.T, m AppModel, msg dataLoadedMsg) AppModel {
	t.Helper()
	updated, _ := m.Update(msg)
	result, ok := updated.(AppModel)
	assert.True(t, ok, "Update should return an AppModel")
	return result
}

func testEntries(count int) []models.UsageEntry {
	entries := make([]models.UsageEntry, count)
	base := time.Now().Add(-30 * time.Minute)
	for i := range entries {
		entries[i] = models.UsageEntry{
			Timestamp:    base.Add(time.Duration(i) * time.Minute),
			InputTokens:  100,
			OutputTokens: 50,
			CostUSD:      0.01,
			Model:        "claude-sonnet-4",
		}
	}
	return entries
}

func TestDataLoadedMsg_ErrorClearsOnSuccessfulLoad(t *testing.T) {
	m := *NewModel(models.DefaultConfig())

	// Failed load shows the error
	m = applyMsg(t, m, dataLoadedMsg{err: errors.New("jsonl read failed")})
	assert.True(t, m.HasError(), "model should show error after failed load")

	// Subsequent successful load recovers
	m = applyMsg(t, m, dataLoadedMsg{entries: testEntries(3)})
	assert.False(t, m.HasError(), "successful load should clear the error")
	assert.True(t, m.HasData())
}

func TestDataLoadedMsg_OAuthDataRetainedOnLoadError(t *testing.T) {
	m := *NewModel(models.DefaultConfig())

	oauthData := &oauth.UsageData{FetchedAt: time.Now()}
	oauthData.FiveHour.Utilisation = 42.0

	m = applyMsg(t, m, dataLoadedMsg{
		err:            errors.New("jsonl read failed"),
		oauthData:      oauthData,
		oauthFreshData: true,
	})

	assert.True(t, m.HasError(), "JSONL error should still display")
	assert.Equal(t, oauthData, m.GetOAuthData(), "OAuth data from an errored load should be retained")
	assert.False(t, m.lastOAuthFetch.IsZero(), "fresh OAuth fetch timestamp should be recorded")
}

func TestDataLoadedMsg_ErrorPersistsWhileLoadsKeepFailing(t *testing.T) {
	m := *NewModel(models.DefaultConfig())

	m = applyMsg(t, m, dataLoadedMsg{err: errors.New("first failure")})
	m = applyMsg(t, m, dataLoadedMsg{err: errors.New("second failure")})

	assert.True(t, m.HasError())
	assert.Contains(t, m.GetError().Error(), "second failure")
}

func TestDataLoadedMsg_StaleGenerationDropped(t *testing.T) {
	m := *NewModel(models.DefaultConfig())
	m.loadGeneration = 2

	// A slow load stamped with an older generation must be ignored
	m = applyMsg(t, m, dataLoadedMsg{entries: testEntries(3), generation: 1})
	assert.False(t, m.HasData(), "stale-generation load should be dropped")
	assert.True(t, m.IsLoading(), "dropped load should not change loading state")

	// The current generation is applied
	m = applyMsg(t, m, dataLoadedMsg{entries: testEntries(3), generation: 2})
	assert.True(t, m.HasData(), "current-generation load should be applied")
}

func TestLoadDataCmdWithModel_IncrementsGeneration(t *testing.T) {
	m := NewModel(models.DefaultConfig())

	assert.Equal(t, uint64(0), m.loadGeneration)
	_ = loadDataCmdWithModel(m.config, m)
	assert.Equal(t, uint64(1), m.loadGeneration, "dispatch should stamp a fresh generation")
	_ = loadDataCmdWithModel(m.config, m)
	assert.Equal(t, uint64(2), m.loadGeneration)
}

func TestDataLoadedMsg_IdenticalEntriesSkipSessionRebuild(t *testing.T) {
	m := *NewModel(models.DefaultConfig())
	entries := testEntries(5)

	m = applyMsg(t, m, dataLoadedMsg{entries: entries})
	firstSessions := m.GetSessions()
	assert.NotEmpty(t, firstSessions, "first load should build session blocks")

	// Cache hit: the identical slice is delivered again - blocks must be
	// reused (same backing array), not rebuilt by CreateSessionBlocks
	m = applyMsg(t, m, dataLoadedMsg{entries: entries})
	secondSessions := m.GetSessions()
	assert.Equal(t, len(firstSessions), len(secondSessions))
	assert.Same(t, &firstSessions[0], &secondSessions[0],
		"identical entries slice should reuse existing session blocks")
	assert.True(t, secondSessions[0].IsActive, "time-dependent fields should still be refreshed")
	assert.InDelta(t, 0.05, secondSessions[0].CostUSD, 1e-9, "session cost should be recomputed")

	// A different slice (fresh parse) must trigger a full rebuild
	m = applyMsg(t, m, dataLoadedMsg{entries: testEntries(5)})
	rebuilt := m.GetSessions()
	assert.NotSame(t, &firstSessions[0], &rebuilt[0],
		"different entries slice should rebuild session blocks")
}
