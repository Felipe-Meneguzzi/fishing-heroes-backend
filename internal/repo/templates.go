package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"fishingheroes/internal/domain"
)

// Templates — catálogo read-only carregado na RAM no boot (ARCHITECTURE §7.3).
type Templates struct {
	Worlds    map[string]*domain.World
	Locations map[string]*domain.Location
	Fish      map[string]*domain.FishTemplate
	Classes   map[string]*domain.ClassTemplate
	Equipment map[string]*domain.EquipmentTemplate
	Baits     map[string]*domain.BaitTemplate
	Runes     map[string]*domain.RuneTemplate
	Materials map[string]string // id -> nome
	Pets      map[string]*domain.PetTemplate
	Skills    map[string]domain.SkillNode
}

// LoadTemplates lê todas as tabelas de template e monta os agregados de domínio.
func LoadTemplates(ctx context.Context, pool *pgxpool.Pool) (*Templates, error) {
	t := &Templates{
		Worlds:    map[string]*domain.World{},
		Locations: map[string]*domain.Location{},
		Fish:      map[string]*domain.FishTemplate{},
		Classes:   map[string]*domain.ClassTemplate{},
		Equipment: map[string]*domain.EquipmentTemplate{},
		Baits:     map[string]*domain.BaitTemplate{},
		Runes:     map[string]*domain.RuneTemplate{},
		Materials: map[string]string{},
		Pets:      map[string]*domain.PetTemplate{},
		Skills:    map[string]domain.SkillNode{},
	}

	if err := t.loadFish(ctx, pool); err != nil {
		return nil, fmt.Errorf("fish_templates: %w", err)
	}
	spawn, err := loadSpawnTables(ctx, pool, t.Fish)
	if err != nil {
		return nil, fmt.Errorf("spawn_tables: %w", err)
	}
	if err := t.loadWorldsAndLocations(ctx, pool, spawn); err != nil {
		return nil, fmt.Errorf("worlds/locations: %w", err)
	}
	if err := t.loadClasses(ctx, pool); err != nil {
		return nil, fmt.Errorf("class_templates: %w", err)
	}
	if err := t.loadEquipment(ctx, pool); err != nil {
		return nil, fmt.Errorf("equipment_templates: %w", err)
	}
	if err := t.loadBaits(ctx, pool); err != nil {
		return nil, fmt.Errorf("bait_templates: %w", err)
	}
	if err := t.loadRunes(ctx, pool); err != nil {
		return nil, fmt.Errorf("rune_templates: %w", err)
	}
	if err := t.loadMaterials(ctx, pool); err != nil {
		return nil, fmt.Errorf("material_templates: %w", err)
	}
	if err := t.loadPets(ctx, pool); err != nil {
		return nil, fmt.Errorf("pet_templates: %w", err)
	}
	if err := t.loadSkills(ctx, pool); err != nil {
		return nil, fmt.Errorf("skill_node_templates: %w", err)
	}
	return t, nil
}

func parseStats(b []byte) (domain.Stats, error) {
	var s domain.Stats
	if len(b) == 0 {
		return s, nil
	}
	return s, json.Unmarshal(b, &s)
}

func (t *Templates) loadFish(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, category, rarity, min_weight, max_weight,
		stamina, force, gold_value, xp, material_id, rune_template_id, species_id FROM fish_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f domain.FishTemplate
		var mat, rune_, species *string
		if err := rows.Scan(&f.ID, &f.Name, &f.Category, &f.Rarity, &f.MinWeight, &f.MaxWeight,
			&f.Stamina, &f.Force, &f.GoldValue, &f.XP, &mat, &rune_, &species); err != nil {
			return err
		}
		if mat != nil {
			f.MaterialID = *mat
		}
		if rune_ != nil {
			f.RuneID = *rune_
		}
		if species != nil {
			f.SpeciesID = *species
		} else {
			f.SpeciesID = f.ID
		}
		fc := f
		t.Fish[f.ID] = &fc
	}
	return rows.Err()
}

func loadSpawnTables(ctx context.Context, pool *pgxpool.Pool, fish map[string]*domain.FishTemplate) (map[string][]domain.SpawnEntry, error) {
	rows, err := pool.Query(ctx, `SELECT spawn_table_id, fish_template_id, weight FROM spawn_tables`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.SpawnEntry{}
	for rows.Next() {
		var tableID, fishID string
		var weight int
		if err := rows.Scan(&tableID, &fishID, &weight); err != nil {
			return nil, err
		}
		f, ok := fish[fishID]
		if !ok {
			return nil, fmt.Errorf("spawn referencia peixe inexistente: %s", fishID)
		}
		out[tableID] = append(out[tableID], domain.SpawnEntry{Fish: f, Weight: weight})
	}
	return out, rows.Err()
}

func (t *Templates) loadWorldsAndLocations(ctx context.Context, pool *pgxpool.Pool, spawn map[string][]domain.SpawnEntry) error {
	wRows, err := pool.Query(ctx, `SELECT id, name, ordering, act_boss_id FROM worlds ORDER BY ordering`)
	if err != nil {
		return err
	}
	defer wRows.Close()
	for wRows.Next() {
		w := &domain.World{}
		if err := wRows.Scan(&w.ID, &w.Name, &w.Order, &w.ActBossID); err != nil {
			return err
		}
		t.Worlds[w.ID] = w
	}
	if err := wRows.Err(); err != nil {
		return err
	}

	lRows, err := pool.Query(ctx, `SELECT id, world_id, name, level, spawn_table_id, weather_seed,
		base_bite_time, gold_per_hour, xp_per_hour FROM locations ORDER BY level`)
	if err != nil {
		return err
	}
	defer lRows.Close()
	for lRows.Next() {
		loc := &domain.Location{}
		var spawnTableID string
		var weatherSeed int64
		if err := lRows.Scan(&loc.ID, &loc.WorldID, &loc.Name, &loc.Level, &spawnTableID,
			&weatherSeed, &loc.BaseBiteTime, &loc.GoldPerHour, &loc.XPPerHour); err != nil {
			return err
		}
		loc.WeatherSeed = uint64(weatherSeed)
		loc.SpawnTable = spawn[spawnTableID]
		t.Locations[loc.ID] = loc
		if w, ok := t.Worlds[loc.WorldID]; ok {
			w.Locations = append(w.Locations, loc)
		}
	}
	return lRows.Err()
}

func (t *Templates) loadClasses(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, description, base_stats FROM class_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		c := &domain.ClassTemplate{}
		var stats []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &stats); err != nil {
			return err
		}
		if c.BaseStats, err = parseStats(stats); err != nil {
			return err
		}
		t.Classes[c.ID] = c
	}
	return rows.Err()
}

func (t *Templates) loadEquipment(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, type, roll_ranges, rune_slots, max_durability FROM equipment_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		e := &domain.EquipmentTemplate{}
		var ranges []byte
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &ranges, &e.RuneSlots, &e.MaxDurability); err != nil {
			return err
		}
		if len(ranges) > 0 {
			if err := json.Unmarshal(ranges, &e.RollRanges); err != nil {
				return err
			}
		}
		t.Equipment[e.ID] = e
	}
	return rows.Err()
}

func (t *Templates) loadBaits(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, kind, tier, bonus_stats, charges, durability FROM bait_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		b := &domain.BaitTemplate{}
		var kind string
		var bonus []byte
		if err := rows.Scan(&b.ID, &b.Name, &kind, &b.Tier, &bonus, &b.Charges, &b.Durability); err != nil {
			return err
		}
		b.Kind = domain.BaitKind(kind)
		if b.Bonus, err = parseStats(bonus); err != nil {
			return err
		}
		t.Baits[b.ID] = b
	}
	return rows.Err()
}

func (t *Templates) loadRunes(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, bonus_stats, apply_status FROM rune_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r := &domain.RuneTemplate{}
		var bonus []byte
		var status *string
		if err := rows.Scan(&r.ID, &r.Name, &bonus, &status); err != nil {
			return err
		}
		if r.Bonus, err = parseStats(bonus); err != nil {
			return err
		}
		if status != nil {
			r.ApplyStatus = *status
		}
		t.Runes[r.ID] = r
	}
	return rows.Err()
}

func (t *Templates) loadMaterials(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name FROM material_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		t.Materials[id] = name
	}
	return rows.Err()
}

func (t *Templates) loadPets(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, base_capacity, base_interval, traits FROM pet_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		p := &domain.PetTemplate{}
		var traits []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseCapacity, &p.BaseInterval, &traits); err != nil {
			return err
		}
		if len(traits) > 0 {
			_ = json.Unmarshal(traits, &p.Traits)
		}
		t.Pets[p.ID] = p
	}
	return rows.Err()
}

func (t *Templates) loadSkills(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, name, branch, requires, max_rank, bonus_per_rank, generic FROM skill_node_templates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n domain.SkillNode
		var branch string
		var requires *string
		var bonus []byte
		if err := rows.Scan(&n.ID, &n.Name, &branch, &requires, &n.MaxRank, &bonus, &n.Generic); err != nil {
			return err
		}
		n.Branch = domain.SkillBranch(branch)
		if requires != nil {
			n.Requires = *requires
		}
		if n.BonusPerRank, err = parseStats(bonus); err != nil {
			return err
		}
		t.Skills[n.ID] = n
	}
	return rows.Err()
}
