// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"net/http"
	"time"

	logger "github.com/soulteary/logger-kit"

	apperrors "github.com/soulteary/apt-proxy/internal/errors"
)

// MirrorsHandler handles mirror-related API endpoints.
//
// reloadFunc is required: it is the per-Server reload closure that owns
// distribution-registry reload + mirror refresh. The handler intentionally
// has no package-global fallback so that misconfigured callers fail loudly
// rather than mutating an unrelated Server's state.
type MirrorsHandler struct {
	log        *logger.Logger
	reloadFunc func()
}

// NewMirrorsHandler creates a new MirrorsHandler. reloadFunc is required;
// passing nil makes HandleMirrorsRefresh return 500.
func NewMirrorsHandler(log *logger.Logger, reloadFunc func()) *MirrorsHandler {
	return &MirrorsHandler{
		log:        log,
		reloadFunc: reloadFunc,
	}
}

// HandleMirrorsRefresh triggers distribution config reload and mirror refresh.
func (h *MirrorsHandler) HandleMirrorsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAppError(w, apperrors.New(apperrors.ErrMethodNotAllowed, "Method not allowed"))
		return
	}

	if h.reloadFunc == nil {
		h.log.Error().Msg("mirrors handler has no reload function configured")
		WriteAppError(w, apperrors.New(apperrors.ErrInternal,
			"mirrors handler not wired to a server (missing reloadFunc)"))
		return
	}

	start := time.Now()
	h.reloadFunc()
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
