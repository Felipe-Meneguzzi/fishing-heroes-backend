package domain

// Entidades de estado do jogador (a verdade vive no Postgres; aqui estão as
// formas em memória). O motor Resolve() não usa o Player diretamente — ele
// recebe um BuildSnapshot congelado. CalculateTotalStats é quem produz esse
// snapshot a partir de Base + Classe + Equipamentos + Runas + SkillTree + Aquário.

// RuneTemplate — runa (inscrição) fixa, sem rolagem. Origem: drop/peixe-runa/síntese.
type RuneTemplate struct {
	ID          string
	Name        string
	Bonus       Stats
	ApplyStatus string // "bleed", etc. ("" = nenhum)
}

// EquipmentInstance — instância de vara/molinete/linha do jogador (stats rolados).
type EquipmentInstance struct {
	InstanceID    string
	TemplateID    string
	Type          string // rod, reel, line
	Bonus         Stats  // rolagem server-seeded (crafting híbrido)
	Durability    float64
	MaxDurability float64
	Runes         []*RuneTemplate // runas engastadas (engaste reversível)
	EquippedSlot  string          // rod/reel/line ou "" (no stash)
}

// AquariumDisplay — melhor troféu ofertado por espécie e o bônus global cacheado.
type AquariumDisplay struct {
	SpeciesID string
	Quality   QualityTier
	Bonus     Stats
}

// Player — estado consolidado do jogador.
type Player struct {
	ID        string
	Name      string
	Class     ClassType
	BaseStats Stats

	// Economia e progressão
	Gold        int64
	Level       int
	XP          int64
	SkillPoints int
	SkillTree   map[string]int // nodeID -> rank investido

	// Build atual
	EquippedRod  *EquipmentInstance
	EquippedReel *EquipmentInstance
	EquippedLine *EquipmentInstance
	ActiveBaitID string

	// Inventário (stash)
	Materials map[string]int             // contagens fungíveis
	Runes     map[string]int             // runas não-engastadas (contagem por template)
	Trophies  []TrophyInstance           // instâncias individuais
	Aquarium  map[string]AquariumDisplay // melhor troféu por espécie → buff global

	Filters           []FilterRule
	HighestLocationID string // baseline do modo desligado
}

// CalculateTotalStats aplica Base → Classe → Equipamentos+Runas → Aquário → SkillTree.
// `skills` é o catálogo de nós (templates) carregado na RAM no boot.
func (p *Player) CalculateTotalStats(skills map[string]SkillNode) Stats {
	total := p.BaseStats

	// 1. Multiplicadores/bônus da Classe (aplicados sobre a base).
	switch p.Class {
	case ClassBruiser:
		total.FishingPower *= 1.20 // +20% de força bruta
	case ClassTrapper:
		total.DoubleCatchChance += 0.05 // chance de Pesca Dupla
		total.BaitSpeed += 0.10         // atrai mais rápido
	case ClassMystic:
		total.LuckChance += 0.05
		total.LuckPower += 0.05
	}

	// 2. Equipamentos e runas engastadas.
	for _, eq := range []*EquipmentInstance{p.EquippedRod, p.EquippedReel, p.EquippedLine} {
		if eq == nil {
			continue
		}
		total = addStats(total, eq.Bonus)
		for _, rn := range eq.Runes {
			if rn != nil {
				total = addStats(total, rn.Bonus)
			}
		}
	}

	// 3. Buffs globais do Aquário Monumental.
	for _, d := range p.Aquarium {
		total = addStats(total, d.Bonus)
	}

	// 4. Skill Tree — soma BonusPerRank por rank investido em cada nó.
	for id, rank := range p.SkillTree {
		node, ok := skills[id]
		if !ok || rank <= 0 {
			continue
		}
		if rank > node.MaxRank {
			rank = node.MaxRank
		}
		for i := 0; i < rank; i++ {
			total = addStats(total, node.BonusPerRank)
		}
	}

	return total
}

// --- XP, nível e reparo ---

// LevelForXP devolve o nível correspondente a um total de XP (curva linear simples).
func LevelForXP(xp int64, cfg BalanceConfig) int {
	if cfg.XPPerLevel <= 0 {
		return 1
	}
	return int(xp/cfg.XPPerLevel) + 1
}

// SkillPointsForLevel — pontos de Skill Tree acumulados num dado nível (1 por nível acima do 1).
func SkillPointsForLevel(level int) int {
	if level <= 1 {
		return 0
	}
	return level - 1
}
