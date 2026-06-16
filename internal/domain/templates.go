package domain

// Templates de conteúdo (read-only, carregados na RAM no boot a partir do banco).
// São apenas estruturas de dados — o domínio continua sem I/O.

// World — Mundo (hierarquia: Mundo › Localização). Progressão linear gated por Act Boss.
type World struct {
	ID        string
	Name      string // ex.: "Floresta"
	Order     int
	ActBossID string
	Locations []*Location
}

// ClassTemplate — definição de uma Classe e seus atributos iniciais.
type ClassTemplate struct {
	ID          string
	Name        string
	Description string
	BaseStats   Stats
}

// EquipmentTemplate — molde de vara/molinete/linha (crafting híbrido).
type EquipmentTemplate struct {
	ID            string
	Name          string
	Type          string                // rod, reel, line
	RollRanges    map[string][2]float64 // campo de Stats -> [min,max]
	RuneSlots     int
	MaxDurability float64
}

// BaseStats devolve o ponto médio das faixas — usado para o equipamento inicial
// (o crafting com rolagem server-seeded é uma feature à parte).
func (t EquipmentTemplate) BaseStats() Stats {
	var s Stats
	for k, rg := range t.RollRanges {
		addToStat(&s, k, (rg[0]+rg[1])/2)
	}
	return s
}

// BaitTemplate — molde de isca (consumível / durável / boss).
type BaitTemplate struct {
	ID         string
	Name       string
	Kind       BaitKind
	Tier       int
	Bonus      Stats
	Charges    *int     // consumível/boss (lote)
	Durability *float64 // durável
}

// PetTemplate — molde de pet transportador (colecionável; um ativo por vez).
type PetTemplate struct {
	ID           string
	Name         string
	BaseCapacity int
	BaseInterval float64
	Traits       []string
}

// addToStat soma `v` ao campo de Stats identificado por `key` (nome do campo Go).
// Usado ao interpretar roll_ranges (que é mapa de string → faixa).
func addToStat(s *Stats, key string, v float64) {
	switch key {
	case "FishingPower":
		s.FishingPower += v
	case "ReelForce":
		s.ReelForce += v
	case "LineTension":
		s.LineTension += v
	case "RodHeight":
		s.RodHeight += v
	case "BaitSpeed":
		s.BaitSpeed += v
	case "DoubleCatchChance":
		s.DoubleCatchChance += v
	case "LuckChance":
		s.LuckChance += v
	case "LuckPower":
		s.LuckPower += v
	case "EscapeReduction":
		s.EscapeReduction += v
	}
}
