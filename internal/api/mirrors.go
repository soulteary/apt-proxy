package api

import (
	"net/http"
	"time"

	logger "github.com/soulteary/logger-kit"

	"github.com/soulteary/apt-proxy/internal/proxy"
)

// MirrorsHandler handles mirror-related API endpoints
type MirrorsHandler struct {
	log *logger.Logger
}

// NewMirrorsHandler creates a new MirrorsHandler
func NewMirrorsHandler(log *logger.Logger) *MirrorsHandler {
	return &MirrorsHandler{
		log: log,
	}
}

// HandleMirrorsRefresh triggers a mirror benchmark refresh
func (h *MirrorsHandler) HandleMirrorsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	start := time.Now()

	// Refresh mirrors using the server reload mechanism
	proxy.RefreshMirrors()

	duration := time.Since(start)

	h.log.Info().
		Dur("duration", duration).
		Msg("mirrors refresh completed")

	resp := MirrorsRefreshResponse{
		Success:    true,
		Message:    "Mirror configurations refreshed",
		DurationMs: duration.Milliseconds(),
	}

	if err := WriteJSON(w, http.StatusOK, resp); err != nil {
		h.log.Error().Err(err).Msg("failed to write mirrors refresh response")
	}
}
