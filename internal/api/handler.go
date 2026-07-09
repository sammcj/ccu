package api

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/analysis"
	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
)

// StateProvider allows BuildStatusResponse to read app state without
// depending directly on the concrete AppModel type.
type StateProvider interface {
	GetOAuthData() *oauth.UsageData
	GetCurrentSession() *models.SessionBlock
	GetSessions() []models.SessionBlock
	GetLimits() models.Limits
	GetConfig() *models.Config
	GetLastRefresh() time.Time
	HasData() bool
}

// BuildStatusResponse assembles a StatusResponse from the current app state
// and serialises it to JSON.
func BuildStatusResponse(state StateProvider, now time.Time) ([]byte, error) {
	limits := state.GetLimits()
	cfg := state.GetConfig()
	oauthData := state.GetOAuthData()
	currentSession := state.GetCurrentSession()
	sessions := state.GetSessions()
	lastRefresh := state.GetLastRefresh()

	resp := StatusResponse{
		Plan:           strings.ToLower(limits.PlanName),
		ServerTime:     now.UTC().Format(time.RFC3339),
		DataAgeSeconds: int(now.Sub(lastRefresh).Seconds()),
	}

	// Weekly section – OAuth only
	if oauthData != nil {
		resp.Weekly = buildWeeklySection(oauthData, cfg, now)
	}

	// Session section
	if currentSession != nil && !currentSession.IsGap {
		resp.Session = buildSessionSection(currentSession, limits, oauthData, now)
	}

	// Burn rate section
	resp.BurnRate = buildBurnRateSection(currentSession, sessions, now)

	// Prediction section
	resp.Prediction = buildPredictionSection(currentSession, oauthData, limits, now)

	return json.Marshal(resp)
}

func buildWeeklySection(oauthData *oauth.UsageData, cfg *models.Config, now time.Time) *WeeklySection {
	w := &WeeklySection{}

	// All-models aggregate
	if resetsAt, err := oauth.ParseResetTime(oauthData.SevenDay.ResetsAt); err == nil {
		resetsIn := int64(math.Max(0, resetsAt.Sub(now).Seconds()))
		w.AllModels = &WeeklyAllSection{
			UtilisationPct:  oauthData.SevenDay.Utilisation,
			ResetsAt:        resetsAt.UTC().Format(time.RFC3339),
			ResetsInSeconds: resetsIn,
		}
	}

	// Per-model weekly limits, whichever models Anthropic currently scopes one to
	for _, limit := range oauthData.WeeklyModelLimits() {
		modelName := limit.ModelName()
		section := &WeeklyModelSection{
			Model:          modelName,
			UtilisationPct: limit.Percent,
		}
		if surface := limit.SurfaceName(); surface != "" {
			section.Surface = surface
		}

		if limitHours := models.WeeklyHoursForModel(cfg.Plan, modelName); limitHours > 0 {
			section.LimitHours = limitHours
			section.UsedHours = limit.Percent / 100.0 * limitHours
		}

		if limit.ResetsAt != nil {
			if resetsAt, err := oauth.ParseResetTime(*limit.ResetsAt); err == nil {
				resetsIn := int64(math.Max(0, resetsAt.Sub(now).Seconds()))
				section.ResetsAt = resetsAt.UTC().Format(time.RFC3339)
				section.ResetsInSeconds = &resetsIn
			}
		}

		if w.Scoped == nil {
			w.Scoped = make(map[string]*WeeklyModelSection)
		}
		// Keyed by oauth.Limit.Key so a model with separate per-surface limits
		// yields separate entries rather than overwriting itself.
		w.Scoped[limit.Key()] = section
	}

	return w
}

func buildSessionSection(session *models.SessionBlock, limits models.Limits, oauthData *oauth.UsageData, now time.Time) *SessionSection {
	elapsed := session.ElapsedDuration(now)
	total := session.Duration()
	remaining := session.RemainingDuration(now)

	// Prefer the OAuth utilisation percentage — it matches what Anthropic's servers
	// track and is what the TUI displays. EffectiveFiveHour applies the same
	// session-rollover staleness clamp the TUI uses: utilisation left over from
	// the previous 5-hour window reads as 0 until the API catches up. Fall back
	// to a cost-based estimate only when OAuth data isn't available.
	var utilisationPct float64
	if oauthData != nil {
		utilisationPct, _, _ = oauthData.EffectiveFiveHour(now)
	} else if limits.CostLimitUSD > 0 {
		utilisationPct = (session.CostUSD / limits.CostLimitUSD) * 100
	}

	remainingSeconds := int64(math.Max(0, remaining.Seconds()))

	var remainingPct float64
	if total > 0 {
		remainingPct = 100 - session.Progress(now)
		if remainingPct < 0 {
			remainingPct = 0
		}
	}

	return &SessionSection{
		UtilisationPct:    utilisationPct,
		ResetsAt:          session.EndTime.UTC().Format(time.RFC3339),
		ResetsInSeconds:   remainingSeconds,
		ElapsedSeconds:    int64(elapsed.Seconds()),
		TotalSeconds:      int64(total.Seconds()),
		RemainingSeconds:  remainingSeconds,
		RemainingPct:      remainingPct,
		CostUSD:           session.CostUSD,
		MessageCount:      session.MessageCount,
		ModelDistribution: buildModelDist(session),
	}
}

func buildModelDist(session *models.SessionBlock) []ModelDistEntry {
	if len(session.PerModelStats) == 0 || session.CostUSD == 0 {
		return []ModelDistEntry{}
	}

	entries := make([]ModelDistEntry, 0, len(session.PerModelStats))
	for model, stats := range session.PerModelStats {
		pct := (stats.CostUSD / session.CostUSD) * 100
		entries = append(entries, ModelDistEntry{
			Model:   models.NormaliseModelName(model),
			CostPct: pct,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CostPct > entries[j].CostPct
	})

	return entries
}

func buildBurnRateSection(currentSession *models.SessionBlock, sessions []models.SessionBlock, now time.Time) *BurnRateSection {
	tokensPerMin := analysis.CalculateBurnRate(sessions, now)

	var costPerMin float64
	if currentSession != nil && !currentSession.IsGap {
		costPerMin = currentSession.CostBurnRate
	}

	return &BurnRateSection{
		TokensPerMin:   tokensPerMin,
		CostPerMinUSD:  costPerMin,
		CostPerHourUSD: costPerMin * 60,
	}
}

func buildPredictionSection(
	currentSession *models.SessionBlock,
	oauthData *oauth.UsageData,
	limits models.Limits,
	now time.Time,
) *PredictionSection {
	pred := &PredictionSection{}

	// Session cost depletion
	if currentSession != nil && !currentSession.IsGap && currentSession.CostBurnRate > 0 && limits.CostLimitUSD > 0 {
		costRemaining := limits.CostLimitUSD - currentSession.CostUSD
		if costRemaining > 0 {
			depletionTime := analysis.PredictCostDepletion(currentSession.CostBurnRate, costRemaining, now)
			if !depletionTime.IsZero() {
				secs := int64(math.Max(0, depletionTime.Sub(now).Seconds()))
				pred.SessionLimitAt = &depletionTime
				pred.SessionLimitInSeconds = &secs
				pred.SessionWillHitLimit = depletionTime.Before(currentSession.EndTime)
			}
		}
	}

	// Weekly depletion – include timestamp fields whenever a prediction is computable,
	// regardless of whether WillHitLimit is true. The ESP32 uses these for countdowns.
	if oauthData != nil {
		weeklyPred := analysis.PredictWeeklyDepletion(oauthData, now)
		if !weeklyPred.DepletionTime.IsZero() {
			secs := int64(math.Max(0, weeklyPred.DepletionTime.Sub(now).Seconds()))
			pred.WeeklyLimitAt = &weeklyPred.DepletionTime
			pred.WeeklyLimitInSeconds = &secs
		}
		pred.WeeklyWillHitLimit = weeklyPred.WillHitLimit
	}

	return pred
}
