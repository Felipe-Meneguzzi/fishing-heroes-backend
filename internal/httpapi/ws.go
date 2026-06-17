package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"fishingheroes/internal/domain"
	"fishingheroes/internal/repo"
)

const (
	wsTick       = 1500 * time.Millisecond // cadência de stream para animação
	wsClaimEvery = 10                      // persiste (~15s) enquanto o jogador assiste
)

// ws — canal de FOREGROUND (cosmético): faz preview (leitura pura) em intervalos
// e transmite eventos resolvidos + snapshot de estado para o cliente animar.
// Nunca decide nada; a verdade vem do Resolve. Persiste periodicamente (claim) e
// ao desconectar, para o progresso de quem assiste não se perder.
func (h *Handler) ws(w http.ResponseWriter, r *http.Request) {
	claims, err := h.svc.Tokens.Verify(r.URL.Query().Get("token"))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "token inválido ou ausente")
		return
	}
	opts := &websocket.AcceptOptions{}
	if slices.Contains(h.opts.CORSOrigins, "*") {
		opts.InsecureSkipVerify = true // dev / origens liberadas
	} else {
		opts.OriginPatterns = h.opts.CORSOrigins
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer c.CloseNow()

	// CloseRead descarta frames do cliente e cancela o ctx quando a conexão fecha.
	ctx := c.CloseRead(r.Context())
	h.streamSession(ctx, c, claims.PlayerID)
}

func (h *Handler) streamSession(ctx context.Context, c *websocket.Conn, playerID string) {
	ticker := time.NewTicker(wsTick)
	defer ticker.Stop()
	lastIdx := -1
	sinceClaim := 0

	for {
		select {
		case <-ctx.Done():
			// Claim final ao desconectar (ctx novo, pois o da conexão já morreu).
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = h.svc.ClaimFishing(cctx, playerID, 0)
			cancel()
			return
		case <-ticker.C:
			rv, err := h.svc.PreviewFishing(ctx, playerID, 0)
			if errors.Is(err, repo.ErrNoSession) {
				_ = wsjson.Write(ctx, c, map[string]any{"type": "idle"})
				continue
			}
			if err != nil {
				return
			}
			// Só eventos novos (índice maior que o último enviado).
			var fresh []domain.GameEvent
			for _, e := range rv.Events {
				if e.Index > lastIdx {
					fresh = append(fresh, e)
					lastIdx = e.Index
				}
			}
			if len(fresh) > 0 {
				if err := wsjson.Write(ctx, c, map[string]any{"type": "events", "events": fresh}); err != nil {
					return
				}
			}
			if err := wsjson.Write(ctx, c, map[string]any{"type": "state", "session": rv.Session, "player": rv.Player}); err != nil {
				return
			}
			if sinceClaim++; sinceClaim >= wsClaimEvery {
				_, _ = h.svc.ClaimFishing(ctx, playerID, 0)
				sinceClaim = 0
			}
		}
	}
}
