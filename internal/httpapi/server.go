// Package httpapi — camada de delivery HTTP (REST). Sem regra de negócio:
// só serialização e chamada aos casos de uso.
package httpapi

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"fishingheroes/internal/repo"
	"fishingheroes/internal/usecase"
)

//go:embed index.html
var indexHTML []byte

// Handler agrega as dependências dos endpoints.
type Handler struct {
	svc *usecase.Service
}

func New(svc *usecase.Service) *Handler { return &Handler{svc: svc} }

// Routes registra as rotas (net/http roteando por método+path, Go 1.22+).
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.home)
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /api/worlds", h.worlds)
	mux.HandleFunc("GET /api/templates", h.templates)
	mux.HandleFunc("POST /api/players", h.createPlayer)
	mux.HandleFunc("GET /api/players/{id}", h.getPlayer)
	// Fluxo de pesca real (Idle): entrar no local, tick ao vivo, sair.
	mux.HandleFunc("POST /api/players/{id}/session/start", h.startSession)
	mux.HandleFunc("POST /api/players/{id}/session/resolve", h.resolveSession)
	mux.HandleFunc("GET /api/players/{id}/session", h.getSession)
	mux.HandleFunc("POST /api/players/{id}/session/stop", h.stopSession)
	return logging(mux)
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (h *Handler) createPlayer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Class string `json:"class"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	p, err := h.svc.NewPlayer(r.Context(), body.Name, body.Class)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) getPlayer(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPlayer(r.Context(), r.PathValue("id"))
	if errors.Is(err, repo.ErrPlayerNotFound) {
		writeErr(w, http.StatusNotFound, "jogador não encontrado")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request) {
	autoRepair := r.URL.Query().Get("autorepair") == "true"
	v, err := h.svc.StartFishing(r.Context(), r.PathValue("id"), r.URL.Query().Get("location"), autoRepair)
	if errors.Is(err, repo.ErrPlayerNotFound) {
		writeErr(w, http.StatusNotFound, "jogador não encontrado")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) resolveSession(w http.ResponseWriter, r *http.Request) {
	var ff float64
	if v := r.URL.Query().Get("ff"); v != "" {
		ff, _ = strconv.ParseFloat(v, 64) // fast-forward de DEV (segundos)
	}
	rv, err := h.svc.ResolveFishing(r.Context(), r.PathValue("id"), ff)
	if errors.Is(err, repo.ErrNoSession) {
		writeErr(w, http.StatusConflict, "nenhuma sessão ativa — entre num local primeiro")
		return
	}
	if errors.Is(err, repo.ErrPlayerNotFound) {
		writeErr(w, http.StatusNotFound, "jogador não encontrado")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rv)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.GetSessionView(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) stopSession(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.StopFishing(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
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

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
