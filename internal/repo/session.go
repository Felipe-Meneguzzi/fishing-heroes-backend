package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fishingheroes/internal/domain"
)

// ErrNoSession — não há sessão de pesca ativa para o jogador.
var ErrNoSession = errors.New("nenhuma sessão de pesca ativa")

// ErrStaleClaim — a âncora da sessão mudou entre o resolve e o commit (claim
// concorrente). O cliente deve reconsultar (preview) e tentar de novo. Evita
// double-credit quando dois claims do mesmo jogador correm em paralelo.
var ErrStaleClaim = errors.New("claim obsoleto — âncora da sessão avançou")

// SessionRow — espelho da linha fishing_session (estado vivo da pesca Idle).
type SessionRow struct {
	PlayerID        string
	Seed            uint64
	StartTime       time.Time
	LocationID      string
	BaitID          string
	BaitChargesLeft *int
	BaitDurability  *float64
	Build           domain.BuildSnapshot
	LastIndex       int
	LastTime        time.Time
	BackpackCount   int
	Durability      float64
	Broken          bool
	AutoRepair      bool
}

// StartSession cria/substitui a sessão do jogador (uma linha por jogador).
func StartSession(ctx context.Context, pool *pgxpool.Pool, row SessionRow) error {
	build, err := json.Marshal(row.Build)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO fishing_session
		(player_id, seed, start_time, location_id, bait_id, bait_charges_left, bait_durability,
		 build_snapshot, last_index, last_time, backpack_count, durability, broken, auto_repair)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,0,$9,0,$10,false,$11)
		ON CONFLICT (player_id) DO UPDATE SET
		 seed=EXCLUDED.seed, start_time=EXCLUDED.start_time, location_id=EXCLUDED.location_id,
		 bait_id=EXCLUDED.bait_id, bait_charges_left=EXCLUDED.bait_charges_left,
		 bait_durability=EXCLUDED.bait_durability, build_snapshot=EXCLUDED.build_snapshot,
		 last_index=0, last_time=EXCLUDED.last_time, backpack_count=0,
		 durability=EXCLUDED.durability, broken=false, auto_repair=EXCLUDED.auto_repair`,
		row.PlayerID, int64(row.Seed), row.StartTime, row.LocationID, row.BaitID,
		row.BaitChargesLeft, row.BaitDurability, build, row.StartTime, row.Durability, row.AutoRepair)
	return err
}

// GetSession lê a sessão ativa do jogador (ErrNoSession se não houver).
func GetSession(ctx context.Context, pool *pgxpool.Pool, playerID string) (*SessionRow, error) {
	row := &SessionRow{PlayerID: playerID}
	var seed int64
	var build []byte
	var baitID *string
	err := pool.QueryRow(ctx, `SELECT seed, start_time, location_id, bait_id, bait_charges_left,
		bait_durability, build_snapshot, last_index, last_time, backpack_count, durability, broken, auto_repair
		FROM fishing_session WHERE player_id = $1`, playerID).Scan(
		&seed, &row.StartTime, &row.LocationID, &baitID, &row.BaitChargesLeft, &row.BaitDurability,
		&build, &row.LastIndex, &row.LastTime, &row.BackpackCount, &row.Durability, &row.Broken, &row.AutoRepair)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	row.Seed = uint64(seed)
	if baitID != nil {
		row.BaitID = *baitID
	}
	if err := json.Unmarshal(build, &row.Build); err != nil {
		return nil, err
	}
	return row, nil
}

// DeleteSession encerra a sessão (sair do local).
func DeleteSession(ctx context.Context, pool *pgxpool.Pool, playerID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM fishing_session WHERE player_id = $1`, playerID)
	return err
}

// SaveResolve persiste, numa única transação (ARCHITECTURE §7.6): os deltas do
// Resolve (ouro líquido de reparos, XP/nível, materiais, troféus, runas) E o
// avanço da sessão (índice/tempo/durabilidade/isca/mochila).
// EquipmentInsert — instância de equipamento a inserir no stash (drop da pesca).
type EquipmentInsert struct {
	TemplateID    string
	Type          string
	Bonus         []byte // JSON de domain.Stats (rolado server-seeded)
	MaxDurability float64
}

func SaveResolve(ctx context.Context, pool *pgxpool.Pool, playerID string, res domain.ResolveResult,
	newLevel, addedSkillPoints int, newHighestLoc string, after SessionRow, expectedIndex int, equips []EquipmentInsert) error {

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Ouro líquido = ganho de venda − gasto de auto-reparo (sink), nunca negativo.
	if _, err := tx.Exec(ctx, `UPDATE players
		SET gold = GREATEST(0, gold + $2 - $3),
		    xp = xp + $4,
		    level = $5,
		    skill_points = skill_points + $6,
		    highest_location_id = COALESCE($7, highest_location_id),
		    updated_at = now()
		WHERE id = $1`,
		playerID, res.Gold, res.RepairsGoldSpent, res.XP, newLevel, addedSkillPoints, nullIfEmpty(newHighestLoc)); err != nil {
		return err
	}

	for matID, qty := range res.Materials {
		if qty == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO player_materials (player_id, material_id, count)
			VALUES ($1,$2,$3) ON CONFLICT (player_id, material_id)
			DO UPDATE SET count = player_materials.count + EXCLUDED.count`, playerID, matID, qty); err != nil {
			return err
		}
	}
	for _, tr := range res.Trophies {
		if _, err := tx.Exec(ctx, `INSERT INTO player_trophies (player_id, species_id, weight, quality, caught_location_id)
			VALUES ($1,$2,$3,$4,$5)`, playerID, tr.SpeciesID, tr.Weight, string(tr.Quality), nullIfEmpty(tr.CaughtLocationID)); err != nil {
			return err
		}
	}
	for runeID, n := range res.RuneDrops {
		if n == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO player_runes (player_id, rune_template_id, count)
			VALUES ($1,$2,$3) ON CONFLICT (player_id, rune_template_id)
			DO UPDATE SET count = player_runes.count + EXCLUDED.count`, playerID, runeID, n); err != nil {
			return err
		}
	}

	// Drops de equipamento (instâncias no stash, com stats rolados server-seeded).
	for _, eq := range equips {
		if _, err := tx.Exec(ctx, `INSERT INTO player_equipment
			(player_id, template_id, type, bonus_stats, durability, max_durability, equipped_slot)
			VALUES ($1,$2,$3,$4,$5,$5,NULL)`,
			playerID, eq.TemplateID, eq.Type, eq.Bonus, eq.MaxDurability); err != nil {
			return err
		}
	}

	// Guarda otimista: só avança se a âncora (last_index) ainda for a que
	// resolvemos. Se outro claim concorrente já avançou, RowsAffected = 0 e a
	// transação inteira (inclusive o crédito de ouro) é desfeita.
	tag, err := tx.Exec(ctx, `UPDATE fishing_session
		SET start_time = $2, last_index = $3, last_time = $4, backpack_count = $5,
		    durability = $6, broken = $7, bait_charges_left = $8, bait_durability = $9
		WHERE player_id = $1 AND last_index = $10`,
		playerID, after.StartTime, after.LastIndex, after.LastTime, after.BackpackCount,
		after.Durability, after.Broken, after.BaitChargesLeft, after.BaitDurability, expectedIndex)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrStaleClaim
	}

	return tx.Commit(ctx)
}
