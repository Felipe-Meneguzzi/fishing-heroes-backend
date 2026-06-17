// Package httpapi — camada de delivery HTTP (REST + WebSocket). Sem regra de
// negócio: só serialização, autenticação e chamada aos casos de uso.
package httpapi

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fishingheroes/internal/auth"
	"fishingheroes/internal/repo"
	"fishingheroes/internal/usecase"
)

//go:embed index.html
var indexHTML []byte

// Options — parâmetros de configuração da camada HTTP.
type Options struct {
	DevMode      bool     // habilita o client de teste em "/" e o fast-forward (ff)
	CORSOrigins  []string // origens permitidas
	MaxBodyBytes int64    // limite do corpo das requisições
}

// Handler agrega as dependências dos endpoints.
type Handler struct {
	svc  *usecase.Service
	opts Options
}

func New(svc *usecase.Service, opts Options) *Handler { return &Handler{svc: svc, opts: opts} }

// Routes registra as rotas e a pilha de middlewares.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Públicas.
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /ready", h.ready)
	mux.HandleFunc("GET /api/worlds", h.worlds)
	mux.HandleFunc("GET /api/templates", h.templates)
	mux.HandleFunc("POST /api/auth/steam", h.authSteam)
	mux.HandleFunc("GET /api/market", h.browseMarket)
	mux.HandleFunc("GET /ws", h.ws) // autentica via ?token=

	// Autenticadas (Bearer → jogador do token).
	mux.HandleFunc("GET /api/me", h.auth(h.me))
	mux.HandleFunc("POST /api/me/session/start", h.auth(h.startSession))
	mux.HandleFunc("GET /api/me/session", h.auth(h.previewSession))
	mux.HandleFunc("POST /api/me/session/claim", h.auth(h.claimSession))
	mux.HandleFunc("POST /api/me/session/stop", h.auth(h.stopSession))
	mux.HandleFunc("GET /api/me/market", h.auth(h.myListings))
	mux.HandleFunc("POST /api/me/market/list", h.auth(h.listItem))
	mux.HandleFunc("POST /api/me/market/buy", h.auth(h.buyItem))
	mux.HandleFunc("POST /api/me/market/cancel", h.auth(h.cancelItem))

	if h.opts.DevMode {
		mux.HandleFunc("GET /{$}", h.home)
	}

	return chain(mux,
		recoverMW,
		loggingMW,
		corsMW(h.opts.CORSOrigins),
		maxBodyMW(h.opts.MaxBodyBytes),
	)
}

// --- autenticação ---

type authedHandler func(w http.ResponseWriter, r *http.Request, c *auth.Claims)

// auth valida o Bearer token e injeta os claims no handler.
func (h *Handler) auth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := h.svc.Tokens.Verify(bearer(r))
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "token inválido ou ausente")
			return
		}
		next(w, r, claims)
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func (h *Handler) authSteam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ticket string `json:"ticket"` // em DevMode, é o próprio SteamID
		Name   string `json:"name"`
		Class  string `json:"class"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	res, err := h.svc.LoginSteam(r.Context(), body.Ticket, body.Name, body.Class)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- saúde / catálogo ---

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Ready(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) worlds(w http.ResponseWriter, r *http.Request) {
	t := h.svc.Templates
	type locView struct {
		ID, Name    string
		Level       int
		GoldPerHour int64
		XPPerHour   int64
	}
	type worldView struct {
		ID, Name  string
		Order     int
		ActBossID string
		Locations []locView
	}
	var out []worldView
	for _, wd := range t.Worlds {
		wv := worldView{ID: wd.ID, Name: wd.Name, Order: wd.Order, ActBossID: wd.ActBossID}
		for _, l := range wd.Locations {
			wv.Locations = append(wv.Locations, locView{l.ID, l.Name, l.Level, l.GoldPerHour, l.XPPerHour})
		}
		out = append(out, wv)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) templates(w http.ResponseWriter, r *http.Request) {
	t := h.svc.Templates
	writeJSON(w, http.StatusOK, map[string]any{
		"classes":   t.Classes,
		"fish":      t.Fish,
		"equipment": t.Equipment,
		"baits":     t.Baits,
		"runes":     t.Runes,
		"materials": t.Materials,
		"pets":      t.Pets,
		"skills":    t.Skills,
	})
}

// --- jogador / sessão ---

func (h *Handler) me(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	p, err := h.svc.GetPlayer(r.Context(), c.PlayerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// devFF lê o fast-forward (segundos) só em DevMode; em produção é sempre 0.
func (h *Handler) devFF(r *http.Request) float64 {
	if !h.opts.DevMode {
		return 0
	}
	ff, _ := strconv.ParseFloat(r.URL.Query().Get("ff"), 64)
	return ff
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	autoRepair := r.URL.Query().Get("autorepair") == "true"
	v, err := h.svc.StartFishing(r.Context(), c.PlayerID, r.URL.Query().Get("location"), autoRepair)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) previewSession(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	rv, err := h.svc.PreviewFishing(r.Context(), c.PlayerID, h.devFF(r))
	if errors.Is(err, repo.ErrNoSession) {
		writeJSON(w, http.StatusOK, map[string]any{"session": map[string]bool{"active": false}})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rv)
}

func (h *Handler) claimSession(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	rv, err := h.svc.ClaimFishing(r.Context(), c.PlayerID, h.devFF(r))
	switch {
	case errors.Is(err, repo.ErrNoSession):
		writeErr(w, http.StatusConflict, "nenhuma sessão ativa — entre num local primeiro")
	case errors.Is(err, repo.ErrStaleClaim):
		writeErr(w, http.StatusConflict, "claim concorrente — reconsulte e tente de novo")
	case err != nil:
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, rv)
	}
}

func (h *Handler) stopSession(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	rv, err := h.svc.StopFishing(r.Context(), c.PlayerID)
	if errors.Is(err, repo.ErrNoSession) {
		writeJSON(w, http.StatusOK, map[string]any{"session": map[string]bool{"active": false}})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rv)
}

// --- marketplace ---

func (h *Handler) browseMarket(w http.ResponseWriter, r *http.Request) {
	listings, err := h.svc.BrowseMarket(r.Context(), r.URL.Query().Get("type"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listings)
}

func (h *Handler) myListings(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	listings, err := h.svc.MyListings(r.Context(), c.PlayerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listings)
}

func (h *Handler) listItem(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	var body struct {
		ItemType string `json:"itemType"`
		ItemID   string `json:"itemId"`
		Price    int64  `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	id, err := h.svc.ListItem(r.Context(), c.PlayerID, body.ItemType, body.ItemID, body.Price)
	if errors.Is(err, repo.ErrItemUnavailable) {
		writeErr(w, http.StatusConflict, "item indisponível para anúncio")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"listingId": id})
}

func (h *Handler) buyItem(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	var body struct {
		ListingID string `json:"listingId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	v, err := h.svc.BuyItem(r.Context(), c.PlayerID, body.ListingID)
	switch {
	case errors.Is(err, repo.ErrListingNotFound):
		writeErr(w, http.StatusNotFound, "anúncio não encontrado")
	case errors.Is(err, repo.ErrInsufficientGold):
		writeErr(w, http.StatusPaymentRequired, "ouro insuficiente")
	case errors.Is(err, repo.ErrSelfPurchase):
		writeErr(w, http.StatusBadRequest, "não é possível comprar o próprio anúncio")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

func (h *Handler) cancelItem(w http.ResponseWriter, r *http.Request, c *auth.Claims) {
	var body struct {
		ListingID string `json:"listingId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	err := h.svc.CancelListing(r.Context(), c.PlayerID, body.ListingID)
	if errors.Is(err, repo.ErrListingNotFound) {
		writeErr(w, http.StatusNotFound, "anúncio não encontrado")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
