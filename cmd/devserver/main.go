// devserver — servidor de desenvolvimento que expõe o motor Resolve() via HTTP
// e serve um cliente HTML cru para visualizar o JSON durante o desenvolvimento.
//
//	go run ./cmd/devserver   →  http://localhost:8080
package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"fishingheroes/internal/domain"
)

//go:embed index.html
var assets embed.FS

type server struct {
	mu      sync.Mutex
	engine  *domain.Engine
	session *domain.Session
	player  playerState
}

// playerState acumula os deltas de cada Resolve (o que seria persistido no claim).
type playerState struct {
	Gold             int64
	XP               int64
	Materials        map[string]int
	Trophies         []domain.TrophyInstance
	EquipmentDrops   map[string]int
	RuneDrops        map[string]int
	TotalEvents      int
	TotalCaught      int
	TotalEscaped     int
	RepairsGoldSpent int64
	StalledSeconds   float64
}

func newPlayer() playerState {
	return playerState{
		Materials:      map[string]int{},
		EquipmentDrops: map[string]int{},
		RuneDrops:      map[string]int{},
	}
}

func main() {
	s := &server{engine: domain.NewEngine(domain.DefaultConfig())}
	s.reset()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/resolve", s.handleResolve)
	mux.HandleFunc("/api/reset", s.handleReset)
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", http.FileServer(http.FS(assets)))

	const addr = ":8080"
	log.Printf("Fishing Heroes devserver → http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *server) reset() {
	s.session = buildSession()
	s.player = newPlayer()
}

// --- handlers ---

func (s *server) handleReset(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	writeJSON(w, s.view(nil))
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.view(nil))
}

// handleResolve avança a sessão. Sem `seconds`, resolve até o tempo real (tick
// ao vivo). Com `seconds=N`, faz fast-forward de N segundos simulados.
func (s *server) handleResolve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if secs, _ := strconv.ParseFloat(r.URL.Query().Get("seconds"), 64); secs > 0 {
		// Fast-forward: empurra o StartTime para trás, aumentando o tempo decorrido.
		s.session.StartTime = s.session.StartTime.Add(-time.Duration(secs * float64(time.Second)))
	}

	var events []domain.GameEvent
	until := time.Since(s.session.StartTime).Seconds()
	res := s.engine.ResolveStream(s.session, until, func(ev domain.GameEvent) {
		events = append(events, ev)
	})
	s.accumulate(res)

	v := s.view(&res)
	v.RecentEvents = lastN(events, 120) // feed do cliente (cap p/ bursts de fast-forward)
	writeJSON(w, v)
}

// handleWS — canal de foreground: a cada segundo resolve o tempo real e
// transmite cada evento + um snapshot de estado para o cliente animar.
func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()

	closed := make(chan struct{})
	go func() { conn.readLoop(); close(closed) }()

	s.mu.Lock()
	st0 := s.view(nil)
	s.mu.Unlock()
	if conn.writeJSON(wsMsg{Type: "state", State: &st0}) != nil {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-closed:
			return
		case <-ticker.C:
			s.mu.Lock()
			var events []domain.GameEvent
			until := time.Since(s.session.StartTime).Seconds()
			res := s.engine.ResolveStream(s.session, until, func(ev domain.GameEvent) {
				events = append(events, ev)
			})
			s.accumulate(res)
			st := s.view(&res)
			s.mu.Unlock()

			for i := range events {
				if conn.writeJSON(wsMsg{Type: "event", Event: &events[i]}) != nil {
					return
				}
			}
			if conn.writeJSON(wsMsg{Type: "state", State: &st}) != nil {
				return
			}
		}
	}
}

func lastN(ev []domain.GameEvent, n int) []domain.GameEvent {
	if len(ev) <= n {
		return ev
	}
	return ev[len(ev)-n:]
}

func (s *server) accumulate(res domain.ResolveResult) {
	p := &s.player
	p.Gold += res.Gold
	p.XP += res.XP
	p.RepairsGoldSpent += res.RepairsGoldSpent
	p.StalledSeconds += res.StalledSeconds
	p.TotalEvents += res.Events
	p.TotalCaught += res.Caught
	p.TotalEscaped += res.Escaped
	for k, v := range res.Materials {
		p.Materials[k] += v
	}
	for k, v := range res.RuneDrops {
		p.RuneDrops[k] += v
	}
	for _, id := range res.EquipmentDrops {
		p.EquipmentDrops[id]++
	}
	p.Trophies = append(p.Trophies, res.Trophies...)
}

// --- views (JSON enxuto para o cliente) ---

type stateView struct {
	Session      sessionView           `json:"session"`
	Player       playerView            `json:"player"`
	LastResult   *domain.ResolveResult `json:"lastResult,omitempty"`
	RecentEvents []domain.GameEvent    `json:"recentEvents,omitempty"`
}

// wsMsg — mensagem enviada pelo canal WebSocket (foreground).
type wsMsg struct {
	Type  string            `json:"type"` // state | event
	Event *domain.GameEvent `json:"event,omitempty"`
	State *stateView        `json:"state,omitempty"`
}

type sessionView struct {
	Class         string  `json:"class"`
	LocationID    string  `json:"locationId"`
	Weather       string  `json:"weatherAgora"`
	RealElapsed   float64 `json:"tempoRealSeg"`
	ResolvedTotal float64 `json:"resolvidoSeg"`
	LastIndex     int     `json:"eventos"`
	Durability    float64 `json:"durabilidade"`
	MaxDurability float64 `json:"durabilidadeMax"`
	Broken        bool    `json:"quebrado"`
	AutoRepair    bool    `json:"autoReparo"`
	BaitKind      string  `json:"iscaTipo"`
	BaitCharges   int     `json:"iscaCargas"`
	BaitStock     int     `json:"iscaEstoque"`
	BaitBasic     bool    `json:"iscaBasica"`
	BackpackCount int     `json:"mochila"`
	BackpackCap   int     `json:"mochilaMax"`
}

type playerView struct {
	Gold             int64                   `json:"ouro"`
	XP               int64                   `json:"xp"`
	Level            int                     `json:"nivel"`
	Materials        map[string]int          `json:"materiais"`
	Trophies         []domain.TrophyInstance `json:"trofeus"`
	EquipmentDrops   map[string]int          `json:"dropEquip"`
	RuneDrops        map[string]int          `json:"dropRunas"`
	TotalEvents      int                     `json:"totalEventos"`
	TotalCaught      int                     `json:"totalCapturados"`
	TotalEscaped     int                     `json:"totalEscapes"`
	RepairsGoldSpent int64                   `json:"ouroEmReparos"`
	StalledSeconds   float64                 `json:"travadoSeg"`
}

func (s *server) view(last *domain.ResolveResult) stateView {
	sess := s.session
	xpPerLevel := s.engine.Cfg.XPPerLevel
	level := 1
	if xpPerLevel > 0 {
		level = int(s.player.XP/xpPerLevel) + 1
	}
	return stateView{
		Session: sessionView{
			Class:         string(sess.Build.Class),
			LocationID:    sess.Location.ID,
			Weather:       string(s.engine.WeatherAt(sess, sess.ElapsedTotal)),
			RealElapsed:   time.Since(sess.StartTime).Seconds(),
			ResolvedTotal: sess.ElapsedTotal,
			LastIndex:     sess.LastIndex,
			Durability:    sess.Durability,
			MaxDurability: sess.Build.MaxDurability,
			Broken:        sess.Broken,
			AutoRepair:    sess.AutoRepair,
			BaitKind:      string(sess.Bait.Kind),
			BaitCharges:   sess.Bait.Charges,
			BaitStock:     sess.Bait.StockCharges,
			BaitBasic:     sess.Bait.Basic,
			BackpackCount: sess.BackpackCount,
			BackpackCap:   sess.BackpackCap,
		},
		Player: playerView{
			Gold:             s.player.Gold,
			XP:               s.player.XP,
			Level:            level,
			Materials:        s.player.Materials,
			Trophies:         s.player.Trophies,
			EquipmentDrops:   s.player.EquipmentDrops,
			RuneDrops:        s.player.RuneDrops,
			TotalEvents:      s.player.TotalEvents,
			TotalCaught:      s.player.TotalCaught,
			TotalEscaped:     s.player.TotalEscaped,
			RepairsGoldSpent: s.player.RepairsGoldSpent,
			StalledSeconds:   s.player.StalledSeconds,
		},
		LastResult: last,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// buildSession — sessão de exemplo (mesma fixture da demo). StartTime = agora,
// então o tick ao vivo avança em tempo real.
func buildSession() *domain.Session {
	vendor := &domain.FishTemplate{ID: "sardine", Name: "Sardinha", Category: domain.CatVendor, Rarity: 0, MinWeight: 0.2, MaxWeight: 1.0, Stamina: 20, Force: 15, GoldValue: 5, XP: 10, SpeciesID: "sardine"}
	material := &domain.FishTemplate{ID: "bonefish", Name: "Peixe-osso", Category: domain.CatMaterial, Rarity: 1, Stamina: 35, Force: 45, XP: 25, MaterialID: "scale"}
	trophy := &domain.FishTemplate{ID: "marlin", Name: "Marlim", Category: domain.CatTrophy, Rarity: 3, MinWeight: 80, MaxWeight: 120, Stamina: 80, Force: 60, XP: 200, SpeciesID: "marlin"}

	loc := &domain.Location{
		ID: "praia_inicial", WorldID: "w1", Level: 1,
		SpawnTable:  []domain.SpawnEntry{{Fish: vendor, Weight: 70}, {Fish: material, Weight: 25}, {Fish: trophy, Weight: 5}},
		WeatherSeed: 0xC0FFEE, BaseBiteTime: 8, GoldPerHour: 1000, XPPerHour: 500,
	}

	return &domain.Session{
		Seed:      uint64(time.Now().UnixNano()),
		StartTime: time.Now(),
		Build: domain.BuildSnapshot{
			Stats: domain.Stats{FishingPower: 50, ReelForce: 20, LineTension: 30, BaitSpeed: 0.2, LuckChance: 0.15, LuckPower: 1, EscapeReduction: 0.2},
			Class: domain.ClassBruiser, MaxDurability: 100,
		},
		Location:    loc,
		Durability:  100,
		AutoRepair:  true,
		Bait:        &domain.BaitState{Kind: domain.BaitConsumable, Bonus: domain.Stats{BaitSpeed: 0.3}, Charges: 500, StockCharges: 5000},
		BackpackCap: 40,
		PetCapacity: 20,
		PetInterval: 120,
	}
}
