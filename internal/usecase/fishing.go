package usecase

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"fishingheroes/internal/domain"
	"fishingheroes/internal/repo"
)

// Fluxo de gameplay real (Idle, jogo aberto): o jogador ENTRA num local
// (StartFishing congela a build e cria a fishing_session) e o personagem pesca
// em TEMPO REAL. O client faz tick periódico (ResolveFishing), que resolve o
// tempo decorrido desde o último claim e persiste os ganhos. `ffSeconds` é um
// atalho de DEV (fast-forward) para simular horas sem esperar.

// SessionView — estado da sessão para a UI.
type SessionView struct {
	Active        bool    `json:"active"`
	LocationID    string  `json:"locationId"`
	LocationName  string  `json:"locationName"`
	Weather       string  `json:"weather"`
	ElapsedSec    float64 `json:"elapsedSec"`
	Events        int     `json:"events"`
	Durability    float64 `json:"durability"`
	MaxDurability float64 `json:"maxDurability"`
	Broken        bool    `json:"broken"`
	BaitID        string  `json:"baitId"`
	BaitKind      string  `json:"baitKind"`
	BaitCharges   int     `json:"baitCharges"`
	BaitBasic     bool    `json:"baitBasic"`
	BackpackCount int     `json:"backpackCount"`
	BackpackCap   int     `json:"backpackCap"`
	AutoRepair    bool    `json:"autoRepair"`
}

// PlayerSummary — números do jogador para a UI.
type PlayerSummary struct {
	Gold        int64 `json:"gold"`
	XP          int64 `json:"xp"`
	Level       int   `json:"level"`
	SkillPoints int   `json:"skillPoints"`
}

// ResolveView — retorno de um tick de pesca.
type ResolveView struct {
	Result  domain.ResolveResult `json:"result"`
	Events  []domain.GameEvent   `json:"events"`
	Session SessionView          `json:"session"`
	Player  PlayerSummary        `json:"player"`
}

const backpackCap = 40

// StartFishing entra num local: congela a build e cria a sessão persistente.
// autoRepair liga o auto-reparo (conserta o equipamento gastando ouro — sink).
func (s *Service) StartFishing(ctx context.Context, playerID, locationID string, autoRepair bool) (*SessionView, error) {
	p, err := s.GetPlayer(ctx, playerID)
	if err != nil {
		return nil, err
	}
	if locationID == "" {
		locationID = p.HighestLocationID
	}
	loc, ok := s.Templates.Locations[locationID]
	if !ok {
		return nil, fmt.Errorf("localização inválida: %s", locationID)
	}

	maxDur, dur := 100.0, 100.0
	if p.EquippedRod != nil {
		maxDur, dur = p.EquippedRod.MaxDurability, p.EquippedRod.Durability
	}
	now := time.Now()
	bt := s.Templates.Baits[p.ActiveBaitID]
	charges, baitDur := initialBait(bt)

	row := repo.SessionRow{
		PlayerID:        playerID,
		Seed:            rand.Uint64(),
		StartTime:       now,
		LocationID:      loc.ID,
		BaitID:          p.ActiveBaitID,
		BaitChargesLeft: charges,
		BaitDurability:  baitDur,
		Build:           domain.BuildSnapshot{Stats: p.CalculateTotalStats(s.Templates.Skills), Class: p.Class, MaxDurability: maxDur},
		LastTime:        now,
		Durability:      dur,
		AutoRepair:      autoRepair,
	}
	if err := repo.StartSession(ctx, s.Pool, row); err != nil {
		return nil, err
	}

	sess, _ := s.sessionFromRow(&row)
	v := s.sessionView(sess, loc, row.BaitID)
	return &v, nil
}

// GetSessionView devolve a sessão ativa (ou Active=false se não houver).
func (s *Service) GetSessionView(ctx context.Context, playerID string) (*SessionView, error) {
	row, err := repo.GetSession(ctx, s.Pool, playerID)
	if err == repo.ErrNoSession {
		return &SessionView{Active: false}, nil
	}
	if err != nil {
		return nil, err
	}
	sess, loc := s.sessionFromRow(row)
	v := s.sessionView(sess, loc, row.BaitID)
	return &v, nil
}

// StopFishing encerra a sessão (sair do local).
func (s *Service) StopFishing(ctx context.Context, playerID string) error {
	return repo.DeleteSession(ctx, s.Pool, playerID)
}

// ResolveFishing resolve o tempo decorrido desde o último claim (+ ffSeconds de
// DEV) e persiste tudo. É o tick do loop de pesca ao vivo.
func (s *Service) ResolveFishing(ctx context.Context, playerID string, ffSeconds float64) (*ResolveView, error) {
	row, err := repo.GetSession(ctx, s.Pool, playerID)
	if err != nil {
		return nil, err
	}
	p, err := s.GetPlayer(ctx, playerID)
	if err != nil {
		return nil, err
	}
	sess, loc := s.sessionFromRow(row)
	sess.Filters = p.Filters

	now := time.Now()
	realDelta := now.Sub(row.LastTime).Seconds()
	if realDelta < 0 {
		realDelta = 0 // sessão "à frente" do relógio (após fast-forward de DEV)
	}
	if ffSeconds < 0 {
		ffSeconds = 0
	}
	until := sess.ElapsedTotal + realDelta + ffSeconds

	var events []domain.GameEvent
	res := s.Engine.ResolveStream(sess, until, func(ev domain.GameEvent) { events = append(events, ev) })

	// Progressão.
	newXP := p.XP + res.XP
	newLevel := domain.LevelForXP(newXP, s.Engine.Cfg)
	addedPoints := domain.SkillPointsForLevel(newLevel) - domain.SkillPointsForLevel(p.Level)
	if addedPoints < 0 {
		addedPoints = 0
	}
	newHighest := ""
	if cur, ok := s.Templates.Locations[p.HighestLocationID]; !ok || loc.Level > cur.Level {
		newHighest = loc.ID
	}

	// Sessão avançada (start_time inalterado; last_time = start + elapsed sim).
	charges, baitDur := baitAfter(sess.Bait)
	after := *row
	after.LastIndex = sess.LastIndex
	after.LastTime = row.StartTime.Add(time.Duration(sess.ElapsedTotal * float64(time.Second)))
	after.BackpackCount = sess.BackpackCount
	after.Durability = sess.Durability
	after.Broken = sess.Broken
	after.BaitChargesLeft = charges
	after.BaitDurability = baitDur

	if err := repo.SaveResolve(ctx, s.Pool, playerID, res, newLevel, addedPoints, newHighest, after); err != nil {
		return nil, err
	}

	gold := p.Gold + res.Gold - res.RepairsGoldSpent
	if gold < 0 {
		gold = 0
	}
	return &ResolveView{
		Result:  res,
		Events:  lastN(events, 200),
		Session: s.sessionView(sess, loc, row.BaitID),
		Player:  PlayerSummary{Gold: gold, XP: newXP, Level: newLevel, SkillPoints: p.SkillPoints + addedPoints},
	}, nil
}

// --- helpers ---

// sessionFromRow reconstrói a domain.Session a partir da linha persistida.
func (s *Service) sessionFromRow(row *repo.SessionRow) (*domain.Session, *domain.Location) {
	loc := s.Templates.Locations[row.LocationID]
	elapsed := row.LastTime.Sub(row.StartTime).Seconds() // sim-segundos já resolvidos
	if elapsed < 0 {
		elapsed = 0
	}
	cap_, interval := 12, 60.0
	if pet, ok := s.Templates.Pets[repo.StarterKit.Pet]; ok {
		cap_, interval = pet.BaseCapacity, pet.BaseInterval
	}
	hauls := 0
	if interval > 0 {
		hauls = int(elapsed / interval)
	}
	return &domain.Session{
		Seed:          row.Seed,
		StartTime:     row.StartTime,
		Build:         row.Build,
		Location:      loc,
		LastIndex:     row.LastIndex,
		ElapsedTotal:  elapsed,
		Durability:    row.Durability,
		Broken:        row.Broken,
		AutoRepair:    row.AutoRepair,
		Bait:          s.baitStateFromRow(row),
		BackpackCount: row.BackpackCount,
		BackpackCap:   backpackCap,
		PetCapacity:   cap_,
		PetInterval:   interval,
		PetHauls:      hauls,
	}, loc
}

func (s *Service) sessionView(sess *domain.Session, loc *domain.Location, baitID string) SessionView {
	return SessionView{
		Active:        true,
		LocationID:    loc.ID,
		LocationName:  loc.Name,
		Weather:       string(s.Engine.WeatherAt(sess, sess.ElapsedTotal)),
		ElapsedSec:    sess.ElapsedTotal,
		Events:        sess.LastIndex,
		Durability:    sess.Durability,
		MaxDurability: sess.Build.MaxDurability,
		Broken:        sess.Broken,
		BaitID:        baitID,
		BaitKind:      string(sess.Bait.Kind),
		BaitCharges:   sess.Bait.Charges,
		BaitBasic:     sess.Bait.Basic,
		BackpackCount: sess.BackpackCount,
		BackpackCap:   sess.BackpackCap,
		AutoRepair:    sess.AutoRepair,
	}
}

func (s *Service) baitStateFromRow(row *repo.SessionRow) *domain.BaitState {
	bt := s.Templates.Baits[row.BaitID]
	if bt == nil {
		return &domain.BaitState{Kind: domain.BaitConsumable, Charges: 1, Basic: true}
	}
	b := &domain.BaitState{Kind: bt.Kind, Bonus: bt.Bonus}
	switch bt.Kind {
	case domain.BaitConsumable:
		if row.BaitChargesLeft != nil {
			b.Charges = *row.BaitChargesLeft
		}
		b.Basic = b.Charges <= 0
	case domain.BaitDurable:
		if row.BaitDurability != nil {
			b.Durability = *row.BaitDurability
		}
		if bt.Durability != nil {
			b.MaxDur = *bt.Durability
		}
		b.Broken = b.Durability <= 0
	}
	return b
}

func initialBait(bt *domain.BaitTemplate) (*int, *float64) {
	if bt == nil {
		return nil, nil
	}
	switch bt.Kind {
	case domain.BaitConsumable:
		c := 0
		if bt.Charges != nil {
			c = *bt.Charges
		}
		return &c, nil
	case domain.BaitDurable:
		d := 0.0
		if bt.Durability != nil {
			d = *bt.Durability
		}
		return nil, &d
	}
	return nil, nil
}

func baitAfter(b *domain.BaitState) (*int, *float64) {
	switch b.Kind {
	case domain.BaitConsumable:
		c := b.Charges
		return &c, nil
	case domain.BaitDurable:
		d := b.Durability
		return nil, &d
	}
	return nil, nil
}

func lastN(ev []domain.GameEvent, n int) []domain.GameEvent {
	if len(ev) <= n {
		return ev
	}
	return ev[len(ev)-n:]
}
