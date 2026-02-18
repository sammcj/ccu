package api

import "time"

// StatusResponse is the top-level response for GET /api/status
type StatusResponse struct {
	Plan            string            `json:"plan"`
	ServerTime      string            `json:"server_time"`
	DataAgeSeconds  int               `json:"data_age_seconds"`
	Weekly          *WeeklySection    `json:"weekly,omitempty"`
	Session         *SessionSection   `json:"session,omitempty"`
	BurnRate        *BurnRateSection  `json:"burn_rate,omitempty"`
	Prediction      *PredictionSection `json:"prediction,omitempty"`
}

// WeeklySection holds weekly usage data broken down by model tier
type WeeklySection struct {
	AllModels *WeeklyAllSection   `json:"all_models,omitempty"`
	Sonnet    *WeeklyModelSection `json:"sonnet,omitempty"`
	Opus      *WeeklyModelSection `json:"opus,omitempty"`
}

// WeeklyAllSection holds aggregate weekly usage across all models
type WeeklyAllSection struct {
	UtilisationPct    float64 `json:"utilisation_pct"`
	ResetsAt          string  `json:"resets_at"`
	ResetsInSeconds   int64   `json:"resets_in_seconds"`
}

// WeeklyModelSection holds weekly usage for a specific model tier
type WeeklyModelSection struct {
	UtilisationPct    float64 `json:"utilisation_pct"`
	UsedHours         float64 `json:"used_hours"`
	LimitHours        float64 `json:"limit_hours"`
	ResetsAt          string  `json:"resets_at"`
	ResetsInSeconds   int64   `json:"resets_in_seconds"`
}

// SessionSection holds current 5-hour session data
type SessionSection struct {
	UtilisationPct    float64            `json:"utilisation_pct"`
	ResetsAt          string             `json:"resets_at"`
	ResetsInSeconds   int64              `json:"resets_in_seconds"`
	ElapsedSeconds    int64              `json:"elapsed_seconds"`
	TotalSeconds      int64              `json:"total_seconds"`
	RemainingSeconds  int64              `json:"remaining_seconds"`
	RemainingPct      float64            `json:"remaining_pct"`
	CostUSD           float64            `json:"cost_usd"`
	MessageCount      int                `json:"message_count"`
	ModelDistribution []ModelDistEntry   `json:"model_distribution"`
}

// ModelDistEntry holds the cost percentage for a single model within a session
type ModelDistEntry struct {
	Model   string  `json:"model"`
	CostPct float64 `json:"cost_pct"`
}

// BurnRateSection holds current token and cost burn rates
type BurnRateSection struct {
	TokensPerMin     float64 `json:"tokens_per_min"`
	CostPerHourUSD   float64 `json:"cost_per_hour_usd"`
	CostPerMinUSD    float64 `json:"cost_per_min_usd"`
}

// PredictionSection holds depletion predictions for session and weekly limits
type PredictionSection struct {
	SessionLimitAt        *time.Time `json:"session_limit_at,omitempty"`
	SessionLimitInSeconds *int64     `json:"session_limit_in_seconds,omitempty"`
	SessionWillHitLimit   bool       `json:"session_will_hit_limit"`
	WeeklyLimitAt         *time.Time `json:"weekly_limit_at,omitempty"`
	WeeklyLimitInSeconds  *int64     `json:"weekly_limit_in_seconds,omitempty"`
	WeeklyWillHitLimit    bool       `json:"weekly_will_hit_limit"`
}
