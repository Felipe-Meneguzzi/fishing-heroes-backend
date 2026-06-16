package domain

// Troféus por tamanho — o servidor sorteia o peso do peixe no instante da
// captura por uma DISTRIBUIÇÃO NORMAL (curva de Gauss): a maioria fica perto do
// tamanho médio e os extremos (troféus grandes / espécimes perfeitos) são raros.
// A fração desse peso em relação ao MÁXIMO da espécie decide a faixa do troféu;
// abaixo do limiar mínimo o peixe não vira troféu (auto-venda por ouro).
// Ver QualityTier e BalanceConfig.Trophy*Pct / WeightSigmaDivisor.

// rollWeight sorteia o tamanho do peixe ~ N(média, σ), clampado a [min, max].
// média = ponto médio da faixa; σ = (max−min)/WeightSigmaDivisor.
func (e *Engine) rollWeight(f *FishTemplate, r *rng) float64 {
	if f.MaxWeight <= f.MinWeight {
		return f.MaxWeight
	}
	div := e.Cfg.WeightSigmaDivisor
	if div <= 0 {
		div = 6
	}
	mean := (f.MinWeight + f.MaxWeight) / 2
	sigma := (f.MaxWeight - f.MinWeight) / div
	w := mean + r.NormFloat64()*sigma
	return clamp(w, f.MinWeight, f.MaxWeight)
}

// classifyTrophy devolve a faixa de qualidade para um peso, ou ("", false) se o
// peixe não atingiu o limiar mínimo de troféu (deve ser vendido).
func (cfg BalanceConfig) classifyTrophy(weight, maxWeight float64) (QualityTier, bool) {
	if maxWeight <= 0 {
		return "", false
	}
	pct := weight / maxWeight
	switch {
	case pct >= cfg.TrophyPerfectPct:
		return QualityPerfect, true
	case pct >= cfg.TrophyLegendaryPct:
		return QualityLegendary, true
	case pct >= cfg.TrophyEpicPct:
		return QualityEpic, true
	case pct >= cfg.TrophyRarePct:
		return QualityRare, true
	case pct >= cfg.TrophyCommonPct:
		return QualityCommon, true
	default:
		return "", false
	}
}

// ClassifyTrophy é a versão exportada (UI/use-cases). Mesma regra do motor.
func (cfg BalanceConfig) ClassifyTrophy(weight, maxWeight float64) (QualityTier, bool) {
	return cfg.classifyTrophy(weight, maxWeight)
}
