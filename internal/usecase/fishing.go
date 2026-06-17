package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"fishingheroes/internal/domain"
	"fishingheroes/internal/repo"
)

// Fluxo de gameplay real (Idle, jogo aberto): o jogador ENTRA num local
// (StartFishing congela a build e cria a fishing_session) e o personagem pesca
// em TEMPO REAL.
//
// Leitura × escrita (escala p/ milhares de jogadores simultâneos):
//   - Preview (GET): recalcula da seed o estado acumulado desde o último claim e
//     devolve SEM tocar no banco. É o que o cliente consulta a cada tick — função
//     pura, custo só de CPU (sub-ms), ZERO escrita no Postgres.
//   - Claim (POST): persiste o acumulado numa transação e avança a âncora. Deve
//     ser chamado com pouca frequência (periódico, ao sair, mochila cheia).
//
// Assim o Postgres só recebe escrita no claim — honra o "zero escrita por tick".
// `ffSeconds` é fast-forward de DEV (cheat); o handler só o repassa em DevMode.

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
	// Popula o cache quente já no start (sem leitura extra: baseline vem de `p`).
	repo.CacheSetSession(ctx, s.Redis, playerID, &repo.HotSession{Row: row, Baseline: repo.PlayerBaseline{
		Gold: p.Gold, XP: p.XP, Level: p.Level, SkillPoints: p.SkillPoints,
		HighestLocationID: p.HighestLocationID, Filters: p.Filters,
	}})

	sess, _ := s.sessionFromRow(&row)
	v := s.sessionView(sess, loc, row.BaitID)
	return &v, nil
}

// PreviewFishing resolve o tempo decorrido e devolve o estado acumulado SEM
// persistir (leitura pura). É o tick consultado pelo cliente para HUD/animação.
func (s *Service) PreviewFishing(ctx context.Context, playerID string, ffSeconds float64) (*ResolveView, error) {
	return s.runResolve(ctx, playerID, ffSeconds, false)
}

// ClaimFishing resolve o tempo decorrido e PERSISTE numa transação, avançando a
// âncora. Chamado de tempos em tempos (não a cada tick).
func (s *Service) ClaimFishing(ctx context.Context, playerID string, ffSeconds float64) (*ResolveView, error) {
	return s.runResolve(ctx, playerID, ffSeconds, true)
}

// StopFishing persiste o progresso pendente (claim) e encerra a sessão.
func (s *Service) StopFishing(ctx context.Context, playerID string) (*ResolveView, error) {
	rv, err := s.runResolve(ctx, playerID, 0, true)
	if errors.Is(err, repo.ErrStaleClaim) {
		// Outro claim concorrente já persistiu o progresso; só encerra.
		if delErr := repo.DeleteSession(ctx, s.Pool, playerID); delErr != nil {
			return nil, delErr
		}
		repo.CacheDelSession(ctx, s.Redis, playerID)
		_ = repo.TouchLogout(ctx, s.Pool, playerID)
		return &ResolveView{Session: SessionView{Active: false}}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := repo.DeleteSession(ctx, s.Pool, playerID); err != nil {
		return nil, err
	}
	repo.CacheDelSession(ctx, s.Redis, playerID)
	// Marca o logout (base do catch-up offline no próximo login).
	_ = repo.TouchLogout(ctx, s.Pool, playerID)
	rv.Session.Active = false
	return rv, nil
}

// runResolve é o núcleo compartilhado de preview/claim: reconstrói a sessão,
// resolve a janela (decorrido real + ffSeconds de DEV) e, se persist, grava.
func (s *Service) runResolve(ctx context.Context, playerID string, ffSeconds float64, persist bool) (*ResolveView, error) {
	hot, err := s.hotSession(ctx, playerID)
	if err != nil {
		return nil, err
	}
	row, base := &hot.Row, &hot.Baseline
	sess, loc := s.sessionFromRow(row)
	sess.Filters = base.Filters

	realDelta := time.Since(row.LastTime).Seconds()
	if realDelta < 0 {
		realDelta = 0 // sessão "à frente" do relógio (após fast-forward de DEV)
	}
	if ffSeconds < 0 {
		ffSeconds = 0
	}
	until := sess.ElapsedTotal + realDelta + ffSeconds

	var events []domain.GameEvent
	res := s.Engine.ResolveStream(sess, until, func(ev domain.GameEvent) { events = append(events, ev) })

	newXP := base.XP + res.XP
	newLevel := domain.LevelForXP(newXP, s.Engine.Cfg)
	addedPoints := domain.SkillPointsForLevel(newLevel) - domain.SkillPointsForLevel(base.Level)
	if addedPoints < 0 {
		addedPoints = 0
	}
	newSkillPoints := base.SkillPoints + addedPoints
	newGold := base.Gold + res.Gold - res.RepairsGoldSpent
	if newGold < 0 {
		newGold = 0
	}
	newHighest := ""
	if cur, ok := s.Templates.Locations[base.HighestLocationID]; !ok || loc.Level > cur.Level {
		newHighest = loc.ID
	}

	if persist {
		charges, baitDur := baitAfter(sess.Bait)
		after := *row
		after.LastIndex = sess.LastIndex
		after.LastTime = row.StartTime.Add(time.Duration(sess.ElapsedTotal * float64(time.Second)))
		after.BackpackCount = sess.BackpackCount
		after.Durability = sess.Durability
		after.Broken = sess.Broken
		after.BaitChargesLeft = charges
		after.BaitDurability = baitDur
		equips := s.buildEquipInserts(res.EquipmentDrops)
		if err := repo.SaveResolve(ctx, s.Pool, playerID, res, newLevel, addedPoints, newHighest, after, row.LastIndex, equips); err != nil {
			if errors.Is(err, repo.ErrStaleClaim) {
				repo.CacheDelSession(ctx, s.Redis, playerID) // cache obsoleto → repovoa do PG
			}
			return nil, err
		}
		// Atualiza o cache quente com a nova âncora + baseline (sem ler o PG).
		hot.Row = after
		hot.Baseline.Gold, hot.Baseline.XP, hot.Baseline.Level, hot.Baseline.SkillPoints = newGold, newXP, newLevel, newSkillPoints
		if newHighest != "" {
			hot.Baseline.HighestLocationID = newHighest
		}
		repo.CacheSetSession(ctx, s.Redis, playerID, hot)
	}

	return &ResolveView{
		Result:  res,
		Events:  lastN(events, 200),
		Session: s.sessionView(sess, loc, row.BaitID),
		Player:  PlayerSummary{Gold: newGold, XP: newXP, Level: newLevel, SkillPoints: newSkillPoints},
	}, nil
}

// hotSession devolve o snapshot quente da sessão: tenta o Redis e, em miss, lê o
// Postgres (âncora + baseline) e popula o cache. ErrNoSession se não houver sessão.
func (s *Service) hotSession(ctx context.Context, playerID string) (*repo.HotSession, error) {
	if h, ok := repo.CacheGetSession(ctx, s.Redis, playerID); ok {
		return h, nil
	}
	row, err := repo.GetSession(ctx, s.Pool, playerID)
	if err != nil {
		return nil, err
	}
	base, err := repo.GetPlayerBaseline(ctx, s.Pool, playerID)
	if err != nil {
		return nil, err
	}
	h := &repo.HotSession{Row: *row, Baseline: *base}
	repo.CacheSetSession(ctx, s.Redis, playerID, h)
	return h, nil
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

// buildEquipInserts rola os stats (server-seeded) de cada drop de equipamento.
func (s *Service) buildEquipInserts(drops []domain.EquipmentDrop) []repo.EquipmentInsert {
	if len(drops) == 0 {
		return nil
	}
	out := make([]repo.EquipmentInsert, 0, len(drops))
	for _, d := range drops {
		et, ok := s.Templates.Equipment[d.TemplateID]
		if !ok {
			continue
		}
		bonus, _ := json.Marshal(domain.RollEquipmentStats(et.RollRanges, d.RollSeed))
		out = append(out, repo.EquipmentInsert{
			TemplateID: et.ID, Type: et.Type, Bonus: bonus, MaxDurability: et.MaxDurability,
		})
	}
	return out
}

func lastN(ev []domain.GameEvent, n int) []domain.GameEvent {
	if len(ev) <= n {
		return ev
	}
	return ev[len(ev)-n:]
}
