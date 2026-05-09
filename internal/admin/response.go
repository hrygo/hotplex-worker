// Package admin implements the HTTP administrative API endpoints.
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// respondJSON writes data as JSON to the response.
func respondJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("admin: failed to encode JSON response", "err", err)
	}
}
