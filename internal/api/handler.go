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

	// Sonnet model tier
	if oauthData.SevenDaySonnet != nil {
		weeklyLimits := models.PredefinedWeeklyLimits[cfg.Plan]
		limitHours := weeklyLimits.SonnetHours
		if limitHours > 0 {
			usedHours := oauthData.SevenDaySonnet.Utilisation / 100.0 * limitHours
			if resetsAt, err := oauth.ParseResetTime(oauthData.SevenDaySonnet.ResetsAt); err == nil {
				resetsIn := int64(math.Max(0, resetsAt.Sub(now).Seconds()))
				w.Sonnet = &WeeklyModelSection{
					UtilisationPct:  oauthData.SevenDaySonnet.Utilisation,
					UsedHours:       usedHours,
					LimitHours:      limitHours,
					ResetsAt:        resetsAt.UTC().Format(time.RFC3339),
					ResetsInSeconds: resetsIn,
				}
			}
		}
	}

	// Opus model tier – only when ResetsAt is present (Anthropic enforcing it)
	if oauthData.SevenDayOpus != nil && oauthData.SevenDayOpus.ResetsAt != nil {
		weeklyLimits := models.PredefinedWeeklyLimits[cfg.Plan]
		limitHours := weeklyLimits.OpusHours
		if resetsAt, err := oauth.ParseResetTime(*oauthData.SevenDayOpus.ResetsAt); err == nil {
			resetsIn := int64(math.Max(0, resetsAt.Sub(now).Seconds()))
			var usedHours float64
			if limitHours > 0 {
				usedHours = oauthData.SevenDayOpus.Utilisation / 100.0 * limitHours
			}
			w.Opus = &WeeklyModelSection{
				UtilisationPct:  oauthData.SevenDayOpus.Utilisation,
				UsedHours:       usedHours,
				LimitHours:      limitHours,
				ResetsAt:        resetsAt.UTC().Format(time.RFC3339),
				ResetsInSeconds: resetsIn,
			}
		}
	}

	return w
}

func buildSessionSection(session *models.SessionBlock, limits models.Limits, oauthData *oauth.UsageData, now time.Time) *SessionSection {
	elapsed := session.ElapsedDuration(now)
	total := session.Duration()
	remaining := session.RemainingDuration(now)

	// Prefer the OAuth utilisation percentage — it matches what Anthropic's servers
	// track and is what the TUI displays. Fall back to a cost-based estimate only
	// when OAuth data isn't available.
	var utilisationPct float64
	if oauthData != nil {
		utilisationPct = oauthData.FiveHour.Utilisation
	} else if limits.CostLimitUSD > 0 {
		utilisationPct = (session.CostUSD / limits.CostLimitUSD) * 100
	}

	// Apply the same staleness clamp the TUI uses in renderSessionMetricsFromOAuth.
	// When the 5-hour window has rolled over, the OAuth API may still report the
	// previous session's utilisation. If it's implausibly high for the elapsed time
	// in the new session, clamp to 0 until the API catches up.
	if oauthData != nil {
		if resetTime, err := oauth.ParseResetTime(oauthData.FiveHour.ResetsAt); err == nil {
			if !resetTime.After(now) { // session has rolled over
				sessionStart := resetTime
				elapsed := now.Sub(sessionStart)
				maxReasonable := (elapsed.Hours() / 5.0) * 100
				if maxReasonable < 1 {
					maxReasonable = 1
				}
				if utilisationPct > maxReasonable*2 {
					utilisationPct = 0
				}
			}
		}
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
		UtilisationPct:   utilisationPct,
		ResetsAt:         session.EndTime.UTC().Format(time.RFC3339),
		ResetsInSeconds:  remainingSeconds,
		ElapsedSeconds:   int64(elapsed.Seconds()),
		TotalSeconds:     int64(total.Seconds()),
		RemainingSeconds: remainingSeconds,
		RemainingPct:     remainingPct,
		CostUSD:          session.CostUSD,
		MessageCount:     session.MessageCount,
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
		weeklyPred := analysis.PredictWeeklyDepletion(oauthData, 0, 0, now)
		if !weeklyPred.DepletionTime.IsZero() {
			secs := int64(math.Max(0, weeklyPred.DepletionTime.Sub(now).Seconds()))
			pred.WeeklyLimitAt = &weeklyPred.DepletionTime
			pred.WeeklyLimitInSeconds = &secs
		}
		pred.WeeklyWillHitLimit = weeklyPred.WillHitLimit
	}

	return pred
}
