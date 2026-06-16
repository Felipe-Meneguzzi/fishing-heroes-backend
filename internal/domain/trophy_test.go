package domain

import "testing"

func TestClassifyTrophyTiers(t *testing.T) {
	cfg := DefaultConfig()
	const max = 100.0
	cases := []struct {
		weight   float64
		want     QualityTier
		isTrophy bool
	}{
		{79, "", false},              // < 80% → não é troféu (auto-venda)
		{80, QualityCommon, true},    // 80%
		{84.9, QualityCommon, true},  // ainda comum
		{85, QualityRare, true},      // 85%
		{90, QualityEpic, true},      // 90%
		{95, QualityLegendary, true}, // 95%
		{100, QualityPerfect, true},  // 100%
		{120, QualityPerfect, true},  // acima do máximo (segurança)
	}
	for _, c := range cases {
		got, ok := cfg.ClassifyTrophy(c.weight, max)
		if ok != c.isTrophy || got != c.want {
			t.Errorf("ClassifyTrophy(%.1f/%.0f) = (%q,%v); quero (%q,%v)",
				c.weight, max, got, ok, c.want, c.isTrophy)
		}
	}
}

func TestCalculateTotalStats(t *testing.T) {
	skills := map[string]SkillNode{
		"skl_core":  {ID: "skl_core", MaxRank: 5, BonusPerRank: Stats{FishingPower: 2}},
		"skl_power": {ID: "skl_power", MaxRank: 10, BonusPerRank: Stats{FishingPower: 4}},
	}
	p := &Player{
		Class:     ClassBruiser,
		BaseStats: Stats{FishingPower: 10, LineTension: 5},
		SkillTree: map[string]int{"skl_core": 2, "skl_power": 3}, // +4 +12 = +16
		EquippedRod: &EquipmentInstance{
			Bonus: Stats{FishingPower: 5},
			Runes: []*RuneTemplate{{Bonus: Stats{FishingPower: 6}}},
		},
		Aquarium: map[string]AquariumDisplay{
			"dourado": {Bonus: Stats{LineTension: 3}},
		},
	}
	got := p.CalculateTotalStats(skills)

	// Base 10 ×1.20 (Brutamontes) = 12; +5 equip +6 runa +16 skills = 39
	if got.FishingPower != 39 {
		t.Errorf("FishingPower = %.2f; quero 39", got.FishingPower)
	}
	// LineTension: 5 base + 3 aquário = 8
	if got.LineTension != 8 {
		t.Errorf("LineTension = %.2f; quero 8", got.LineTension)
	}
}
