package api

import (
	"net/http"
	"time"

	logger "github.com/soulteary/logger-kit"

	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/internal/proxy"
)

// MirrorsHandler handles mirror-related API endpoints
type MirrorsHandler struct {
	log        *logger.Logger
	reloadFunc func() // optional: reload distributions config then refresh mirrors
}

// NewMirrorsHandler creates a new MirrorsHandler. reloadFunc is optional; if set,
// HandleMirrorsRefresh will call it (e.g. reload distributions.yaml then refresh mirrors).
func NewMirrorsHandler(log *logger.Logger, reloadFunc func()) *MirrorsHandler {
	return &MirrorsHandler{
		log:        log,
		reloadFunc: reloadFunc,
	}
}

// HandleMirrorsRefresh triggers distribution config reload and mirror refresh
func (h *MirrorsHandler) HandleMirrorsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAppError(w, apperrors.New(apperrors.ErrMethodNotAllowed, "Method not allowed"))
		return
	}

	start := time.Now()

	if h.reloadFunc != nil {
		h.reloadFunc()
	} else {
		proxy.RefreshMirrors()
	}

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
