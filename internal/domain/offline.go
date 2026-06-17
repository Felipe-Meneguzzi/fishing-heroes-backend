package domain

import "math"

// OfflineReward — recompensa de catch-up do modo DESLIGADO (app fechado),
// calculada uma vez no login. Fórmula fechada (não simula eventos, não gera
// itens): média de ouro/XP por hora da melhor Localização × (1 − redução),
// limitada a OfflineCapSeconds (8h). Ver GAMEPLAY §6.1 / ARCHITECTURE §5.B.
func OfflineReward(awaySeconds float64, goldPerHour, xpPerHour int64, cfg BalanceConfig) (gold, xp int64) {
	secs := math.Min(awaySeconds, cfg.OfflineCapSeconds)
	if secs <= 0 {
		return 0, 0
	}
	hours := secs / 3600
	frac := cfg.OfflineReductionPct
	gold = int64(hours * float64(goldPerHour) * frac)
	xp = int64(hours * float64(xpPerHour) * frac)
	return gold, xp
}
