package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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

// CreatePlayer cria um jogador com a Classe escolhida e o kit inicial completo
// (vara/molinete/linha, iscas, pet), tudo numa transação.
func CreatePlayer(ctx context.Context, pool *pgxpool.Pool, t *Templates, name, class string) (string, error) {
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
	err = tx.QueryRow(ctx, `INSERT INTO players (name, class, base_stats, active_bait_id, highest_location_id)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		name, class, baseStats, StarterKit.ConsumableBait, StarterKit.StartLocation).Scan(&id)
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
	if err := loadInventory(ctx, pool, p); err != nil {
		return nil, err
	}
	return p, nil
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

	trRows, err := pool.Query(ctx, `SELECT species_id, weight, quality, caught_location_id
		FROM player_trophies WHERE player_id = $1 ORDER BY caught_at DESC LIMIT 200`, p.ID)
	if err != nil {
		return err
	}
	defer trRows.Close()
	for trRows.Next() {
		var tr domain.TrophyInstance
		var q string
		var loc *string
		if err := trRows.Scan(&tr.SpeciesID, &tr.Weight, &q, &loc); err != nil {
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

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
