package domain

// BalanceConfig — constantes de balance do motor. São números 🟢 (tuning):
// ficam aqui para o protótipo rodar; no jogo viriam de config/seed do banco.
type BalanceConfig struct {
	OfflineCapSeconds float64 // janela do modo DESLIGADO (o Idle não usa cap)

	// Pesca
	FightK     float64 // duração da luta = FightK * stamina / poder
	EscapeBase float64 // fração-base do tempo de luta gasto no escape (0.5)

	// Durabilidade
	WearPerCatch    float64 // durabilidade consumida por captura
	BruiserWearMult float64 // multiplicador de desgaste do Brutamontes
	RepairGoldCost  int64   // custo de ouro do auto-reparo (elevado)
	StormWearMult   float64 // tempestade consome mais durabilidade

	// Sorte
	LuckRescueBase float64 // +Y de força no resgate = LuckRescueBase * LuckPower

	// Clima
	WeatherSlotSeconds    float64 // duração de um slot de clima
	WeatherStormPct       int     // % de slots que são tempestade
	WeatherStormForceMult float64 // tempestade aumenta a Força Exigida
	MysticWeatherIgnore   float64 // fração da penalidade de clima ignorada pelo Místico

	// Troféus — limiares em fração do peso MÁXIMO da espécie.
	// O tamanho é sorteado por uma NORMAL (Gauss) centrada no ponto médio de
	// [MinWeight, MaxWeight], com σ = (max−min)/WeightSigmaDivisor (clampado ao
	// intervalo). pct = peso/MaxWeight; abaixo de TrophyCommonPct é auto-venda.
	WeightSigmaDivisor float64 // divisor da amplitude → desvio-padrão (≈6 ⇒ ±3σ cobre a faixa)
	TrophyCommonPct    float64 // ≥ → Comum    (0.80)
	TrophyRarePct      float64 // ≥ → Raro      (0.85)
	TrophyEpicPct      float64 // ≥ → Épico     (0.90)
	TrophyLegendaryPct float64 // ≥ → Lendário  (0.95)
	TrophyPerfectPct   float64 // ≥ → Perfeito  (1.00)

	// Drops raros
	EquipDropChance float64
	RuneDropChance  float64
	EquipDropTable  []string
	RuneDropTable   []string

	// Isca básica (fallback quando a consumível esgota)
	BasicBaitBiteMult float64 // penalidade no intervalo de mordida (>1 = mais lento)

	// Modo quebrado
	BrokenPowerMult float64 // multiplicador de poder com equipamento quebrado (<1)

	// XP
	XPPerLevel int64 // XP por nível (curva linear simples no protótipo)
}

// DefaultConfig devolve valores plausíveis para o protótipo rodar de imediato.
func DefaultConfig() BalanceConfig {
	return BalanceConfig{
		OfflineCapSeconds: 8 * 3600,

		FightK:     2.0,
		EscapeBase: 0.5,

		WearPerCatch:    1.0,
		BruiserWearMult: 2.0,
		RepairGoldCost:  500,
		StormWearMult:   1.5,

		LuckRescueBase: 10.0,

		WeatherSlotSeconds:    3600,
		WeatherStormPct:       25,
		WeatherStormForceMult: 1.5,
		MysticWeatherIgnore:   0.5,

		TrophyCommonPct:    0.80,
		TrophyRarePct:      0.85,
		TrophyEpicPct:      0.90,
		TrophyLegendaryPct: 0.95,
		TrophyPerfectPct:   1.00,
		WeightSigmaDivisor: 6.0,

		EquipDropChance: 0.01,
		RuneDropChance:  0.02,
		EquipDropTable:  []string{"rod_starter", "reel_starter"},
		RuneDropTable:   []string{"rune_barb", "rune_titanio"},

		BasicBaitBiteMult: 1.5,

		BrokenPowerMult: 0.40,

		XPPerLevel: 1000,
	}
}
