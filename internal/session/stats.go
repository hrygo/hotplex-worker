package session

// TurnStats holds statistics for a single turn.
type TurnStats struct {
	Seq        int64          `json:"seq"`
	Success    bool           `json:"success"`
	Dropped    bool           `json:"dropped"`
	DurationMs int64          `json:"duration_ms"`
	CostUSD    float64        `json:"cost_usd"`
	Usage      map[string]any `json:"usage"`
	ModelUsage map[string]any `json:"model_usage"`
	CreatedAt  string         `json:"created_at"`
}

// SessionStats holds aggregated statistics across all turns of a session.
type SessionStats struct {
	SessionID       string                        `json:"session_id"`
	TotalTurns      int                           `json:"total_turns"`
	SuccessTurns    int                           `json:"success_turns"`
	FailedTurns     int                           `json:"failed_turns"`
	DroppedTurns    int                           `json:"dropped_turns"`
	TotalDurationMs int64                         `json:"total_duration_ms"`
	TotalCostUSD    float64                       `json:"total_cost_usd"`
	TotalUsage      map[string]float64            `json:"total_usage"`
	TotalModelUsage map[string]map[string]float64 `json:"total_model_usage"`
	Turns           []TurnStats                   `json:"turns"`
}
