package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fishingheroes/internal/domain"
)

// ErrPlayerNotFound — jogador inexistente.
var ErrPlayerNotFound = errors.New("jogador não encontrado")

// StarterKit — kit inicial concedido a todo novo jogador (conteúdo do MVP).
var StarterKit = struct {
	Equipment      []string // templates equipados
	ConsumableBait string
	DurableBait    string
	Pet            string
	StartLocation  string
}{
	Equipment:      []string{"rod_starter", "reel_starter", "line_starter"},
	ConsumableBait: "bait_minhoca",
	DurableBait:    "bait_colher",
	Pet:            "pet_corvo",
	StartLocation:  "1-1",
}

// CreatePlayer cria um jogador (vinculado a uma conta) com a Classe escolhida e
// o kit inicial completo (vara/molinete/linha, iscas, pet), tudo numa transação.
func CreatePlayer(ctx context.Context, pool *pgxpool.Pool, t *Templates, accountID, name, class string) (string, error) {
	cls, ok := t.Classes[class]
	if !ok {
		return "", fmt.Errorf("classe inválida: %s", class)
	}
	baseStats, err := json.Marshal(cls.BaseStats)
	if err != nil {
		return "", err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx, `INSERT INTO players (account_id, name, class, base_stats, active_bait_id, highest_location_id)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		nullIfEmpty(accountID), name, class, baseStats, StarterKit.ConsumableBait, StarterKit.StartLocation).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert player: %w", err)
	}

	// Equipamentos iniciais (stats = ponto médio das faixas; durabilidade cheia).
	for _, eqID := range StarterKit.Equipment {
		et, ok := t.Equipment[eqID]
		if !ok {
			return "", fmt.Errorf("equipamento inicial ausente: %s", eqID)
		}
		bonus, _ := json.Marshal(et.BaseStats())
		if _, err := tx.Exec(ctx, `INSERT INTO player_equipment
			(player_id, template_id, type, bonus_stats, durability, max_durability, equipped_slot)
			VALUES ($1,$2,$3,$4,$5,$5,$3)`,
			id, et.ID, et.Type, bonus, et.MaxDurability); err != nil {
			return "", fmt.Errorf("insert equipamento %s: %w", eqID, err)
		}
	}

	// Iscas iniciais (1 consumível + 1 durável).
	if b, ok := t.Baits[StarterKit.ConsumableBait]; ok {
		if _, err := tx.Exec(ctx, `INSERT INTO player_baits (player_id, bait_id, kind, tier, charges, durability)
			VALUES ($1,$2,$3,$4,$5,$6)`, id, b.ID, string(b.Kind), b.Tier, b.Charges, b.Durability); err != nil {
			return "", fmt.Errorf("insert isca consumível: %w", err)
		}
	}
	if b, ok := t.Baits[StarterKit.DurableBait]; ok {
		if _, err := tx.Exec(ctx, `INSERT INTO player_baits (player_id, bait_id, kind, tier, charges, durability)
			VALUES ($1,$2,$3,$4,$5,$6)`, id, b.ID, string(b.Kind), b.Tier, b.Charges, b.Durability); err != nil {
			return "", fmt.Errorf("insert isca durável: %w", err)
		}
	}

	// Pet inicial + ativação.
	var petInstanceID string
	if err := tx.QueryRow(ctx, `INSERT INTO player_pets (player_id, template_id) VALUES ($1,$2) RETURNING id`,
		id, StarterKit.Pet).Scan(&petInstanceID); err != nil {
		return "", fmt.Errorf("insert pet: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE players SET active_pet_id = $2 WHERE id = $1`, id, petInstanceID); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

// GetPlayer carrega o estado do jogador suficiente para montar a build e o resumo.
func GetPlayer(ctx context.Context, pool *pgxpool.Pool, id string) (*domain.Player, error) {
	p := &domain.Player{
		SkillTree: map[string]int{},
		Materials: map[string]int{},
		Runes:     map[string]int{},
		Aquarium:  map[string]domain.AquariumDisplay{},
	}
	var class string
	var baseStats, skillTree, filters []byte
	var activeBait, highestLoc *string

	err := pool.QueryRow(ctx, `SELECT id, name, class, base_stats, gold, level, xp, skill_points,
		skill_tree, filters, active_bait_id, highest_location_id
		FROM players WHERE id = $1`, id).Scan(
		&p.ID, &p.Name, &class, &baseStats, &p.Gold, &p.Level, &p.XP, &p.SkillPoints,
		&skillTree, &filters, &activeBait, &highestLoc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Class = domain.ClassType(class)
	if p.BaseStats, err = parseStats(baseStats); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(skillTree, &p.SkillTree)
	if activeBait != nil {
		p.ActiveBaitID = *activeBait
	}
	if highestLoc != nil {
		p.HighestLocationID = *highestLoc
	}

	if err := loadEquipped(ctx, pool, p); err != nil {
		return nil, err
	}
	if err := loadStashEquipment(ctx, pool, p); err != nil {
		return nil, err
	}
	if err := loadInventory(ctx, pool, p); err != nil {
		return nil, err
	}
	return p, nil
}

// PlayerBaseline — campos do jogador necessários no loop de pesca (sem carregar
// inventário/troféus). É uma única leitura por PK — barata o bastante para o
// caminho quente de 1000+ jogadores.
type PlayerBaseline struct {
	Gold              int64
	XP                int64
	Level             int
	SkillPoints       int
	HighestLocationID string
	Filters           []domain.FilterRule
	LastLogout        *time.Time
}

// GetPlayerBaseline lê só a linha players (sem joins de inventário).
func GetPlayerBaseline(ctx context.Context, pool *pgxpool.Pool, id string) (*PlayerBaseline, error) {
	b := &PlayerBaseline{}
	var filters []byte
	var highestLoc *string
	err := pool.QueryRow(ctx, `SELECT gold, xp, level, skill_points, highest_location_id, filters, last_logout
		FROM players WHERE id = $1`, id).Scan(&b.Gold, &b.XP, &b.Level, &b.SkillPoints, &highestLoc, &filters, &b.LastLogout)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, err
	}
	if highestLoc != nil {
		b.HighestLocationID = *highestLoc
	}
	_ = json.Unmarshal(filters, &b.Filters)
	return b, nil
}

// ApplyOfflineReward credita a recompensa de catch-up e consome o last_logout.
func ApplyOfflineReward(ctx context.Context, pool *pgxpool.Pool, playerID string, gold, xp int64, newLevel, addedPoints int) error {
	_, err := pool.Exec(ctx, `UPDATE players
		SET gold = gold + $2, xp = xp + $3, level = $4, skill_points = skill_points + $5,
		    last_logout = NULL, updated_at = now()
		WHERE id = $1`, playerID, gold, xp, newLevel, addedPoints)
	return err
}

// TouchLogout marca o instante em que o jogador parou (base do catch-up offline).
func TouchLogout(ctx context.Context, pool *pgxpool.Pool, playerID string) error {
	_, err := pool.Exec(ctx, `UPDATE players SET last_logout = now() WHERE id = $1`, playerID)
	return err
}

func loadEquipped(ctx context.Context, pool *pgxpool.Pool, p *domain.Player) error {
	rows, err := pool.Query(ctx, `SELECT id, template_id, type, bonus_stats, durability, max_durability, equipped_slot
		FROM player_equipment WHERE player_id = $1 AND equipped_slot IS NOT NULL`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		eq := &domain.EquipmentInstance{}
		var bonus []byte
		var slot *string
		if err := rows.Scan(&eq.InstanceID, &eq.TemplateID, &eq.Type, &bonus, &eq.Durability, &eq.MaxDurability, &slot); err != nil {
			return err
		}
		if eq.Bonus, err = parseStats(bonus); err != nil {
			return err
		}
		if slot != nil {
			eq.EquippedSlot = *slot
		}
		switch eq.Type {
		case "rod":
			p.EquippedRod = eq
		case "reel":
			p.EquippedReel = eq
		case "line":
			p.EquippedLine = eq
		}
	}
	return rows.Err()
}

func loadInventory(ctx context.Context, pool *pgxpool.Pool, p *domain.Player) error {
	matRows, err := pool.Query(ctx, `SELECT material_id, count FROM player_materials WHERE player_id = $1`, p.ID)
	if err != nil {
		return err
	}
	for matRows.Next() {
		var mid string
		var c int64
		if err := matRows.Scan(&mid, &c); err != nil {
			matRows.Close()
			return err
		}
		p.Materials[mid] = int(c)
	}
	matRows.Close()

	runeRows, err := pool.Query(ctx, `SELECT rune_template_id, count FROM player_runes WHERE player_id = $1`, p.ID)
	if err != nil {
		return err
	}
	for runeRows.Next() {
		var rid string
		var c int
		if err := runeRows.Scan(&rid, &c); err != nil {
			runeRows.Close()
			return err
		}
		p.Runes[rid] = c
	}
	runeRows.Close()

	trRows, err := pool.Query(ctx, `SELECT id, species_id, weight, quality, caught_location_id
		FROM player_trophies WHERE player_id = $1 AND on_market = false ORDER BY caught_at DESC LIMIT 200`, p.ID)
	if err != nil {
		return err
	}
	defer trRows.Close()
	for trRows.Next() {
		var tr domain.TrophyInstance
		var q string
		var loc *string
		if err := trRows.Scan(&tr.ID, &tr.SpeciesID, &tr.Weight, &q, &loc); err != nil {
			return err
		}
		tr.Quality = domain.QualityTier(q)
		if loc != nil {
			tr.CaughtLocationID = *loc
		}
		p.Trophies = append(p.Trophies, tr)
	}
	return trRows.Err()
}

// loadStashEquipment carrega equipamentos no stash (não equipados, fora do mercado).
func loadStashEquipment(ctx context.Context, pool *pgxpool.Pool, p *domain.Player) error {
	rows, err := pool.Query(ctx, `SELECT id, template_id, type, bonus_stats, durability, max_durability
		FROM player_equipment WHERE player_id = $1 AND equipped_slot IS NULL AND on_market = false
		ORDER BY type LIMIT 200`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		eq := &domain.EquipmentInstance{}
		var bonus []byte
		if err := rows.Scan(&eq.InstanceID, &eq.TemplateID, &eq.Type, &bonus, &eq.Durability, &eq.MaxDurability); err != nil {
			return err
		}
		if eq.Bonus, err = parseStats(bonus); err != nil {
			return err
		}
		p.StashEquipment = append(p.StashEquipment, eq)
	}
	return rows.Err()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
