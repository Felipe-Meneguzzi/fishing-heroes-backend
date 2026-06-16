package domain

import "math"

// Engine resolve sessões de pesca. Os templates são read-only; o método Resolve
// não tem efeitos colaterais além de avançar o estado mutável da Session.
type Engine struct {
	Cfg BalanceConfig
}

// NewEngine cria um motor com a config dada.
func NewEngine(cfg BalanceConfig) *Engine { return &Engine{Cfg: cfg} }

// ResolveResult — deltas agregados de um Resolve (aplicados no claim/transação).
type ResolveResult struct {
	Events  int
	Caught  int
	Escaped int

	Gold      int64
	XP        int64
	Materials map[string]int
	Trophies  []TrophyInstance

	EquipmentDrops []string
	RuneDrops      map[string]int

	RepairsGoldSpent int64
	BrokeDuringRun   bool
	StalledSeconds   float64
}

func newResult() ResolveResult {
	return ResolveResult{Materials: map[string]int{}, RuneDrops: map[string]int{}}
}

// GameEvent — um evento de pesca resolvido, para streaming/animação no cliente
// (canal WebSocket). É puramente informativo: a verdade já está no ResolveResult.
type GameEvent struct {
	Index    int             `json:"index"`
	AtSec    float64         `json:"atSec"` // conclusão, em segundos desde StartTime
	Kind     string          `json:"kind"`  // catch | escape
	Fish     string          `json:"fish"`
	Category string          `json:"category"`
	Weather  string          `json:"weather"`
	Gold     int64           `json:"gold,omitempty"`
	Material string          `json:"material,omitempty"`
	Trophy   *TrophyInstance `json:"trophy,omitempty"`
	Broke    bool            `json:"broke,omitempty"`
}

// Resolve avança a sessão resolvendo todos os eventos cuja conclusão caiba até
// `untilElapsed` (segundos desde StartTime). É o mesmo código para o lote online
// e para o retorno do offline-aberto (estado Idle).
//
// Propriedade-chave (aditividade): Resolve(s, T) produz o mesmo estado final e a
// soma dos mesmos deltas que Resolve(s, T1) seguido de Resolve(s, T) com T1 < T,
// porque o estado e o `res` só são tocados em eventos completos (commit). Um
// evento que não cabe inteiro na janela é deferido sem deixar rastro.
func (e *Engine) Resolve(s *Session, untilElapsed float64) ResolveResult {
	return e.resolve(s, untilElapsed, nil)
}

// ResolveStream é como Resolve, mas chama `emit` para cada evento commitado —
// usado pelo WebSocket para alimentar a animação do cliente em tempo real.
func (e *Engine) ResolveStream(s *Session, untilElapsed float64, emit func(GameEvent)) ResolveResult {
	return e.resolve(s, untilElapsed, emit)
}

func (e *Engine) resolve(s *Session, untilElapsed float64, emit func(GameEvent)) ResolveResult {
	res := newResult()
	cfg := e.Cfg
	base := s.ElapsedTotal
	window := untilElapsed - base
	if window <= 0 {
		return res
	}

	// Estado commitado (espelho dos campos mutáveis da Session).
	committedT := 0.0
	idx := s.LastIndex
	bp := s.BackpackCount
	dur := s.Durability
	broken := s.Broken
	hauls := s.PetHauls
	bait := *s.Bait

	for {
		// Cópias temporárias do evento: só viram commitadas se o evento couber.
		t := committedT
		tbp, tdur, tbroken, thauls := bp, dur, broken, hauls
		tbait := bait
		evStall := 0.0

		// Viagens do Pet já devidas liberam espaço.
		processHauls(&thauls, &tbp, base+t, s)

		// STALL pré-mordida: mochila cheia → espera a próxima viagem antes de pescar.
		for s.BackpackCap > 0 && tbp >= s.BackpackCap {
			nh := float64(thauls+1) * s.PetInterval
			if nh-base > window {
				goto done
			}
			evStall += (nh - base) - t
			t = nh - base
			thauls++
			tbp -= imin(tbp, s.PetCapacity)
		}

		{
			r := newEventRNG(s.Seed, uint64(idx))

			// 1. Mordida — intervalo aleatório em [X, 3X].
			xEff := e.biteTime(s, &tbait)
			t += xEff + r.Float64()*2*xEff
			if t > window {
				break
			}
			processHauls(&thauls, &tbp, base+t, s)

			// 2. Clima determinístico no instante da mordida.
			weather := e.weatherAt(s, base+t)

			// 3. Spawn (raridade enviesada pela Sorte; tempestade favorece raros).
			stats := e.effectiveStats(s, &tbait, tbroken)
			fish := e.spawn(s, r, weather, stats)

			// 4. Captura — determinística, com resgate da Sorte.
			power := stats.FishingPower
			required := fish.Force
			if weather == WeatherStorm {
				required *= e.stormForceMult(s)
			}
			caught := power >= required
			if !caught && r.Float64() < stats.LuckChance {
				power += cfg.LuckRescueBase * stats.LuckPower
				caught = power >= required
			}

			// 5. Luta proporcional / escape (50% redutível).
			fightFull := cfg.FightK * fish.Stamina / math.Max(power, 1)
			if caught {
				t += fightFull
			} else {
				t += fightFull * cfg.EscapeBase * (1 - clamp(stats.EscapeReduction, 0, 0.9))
			}
			if t > window {
				break
			}

			var evGold int64
			var evMaterial string
			var evTrophy *TrophyInstance
			var evBroke bool

			if caught {
				// Destino da captura decidido ANTES de tocar em res, para poder
				// deferir o evento (mochila cheia) sem efeitos colaterais.
				//   vendor   → sorteia o tamanho; ≥80% do máximo vira troféu, senão vende
				//   material → conta o material (peixe de crafting)
				//   rune     → concede a runa (peixe-runa)
				//   trophy   → sempre troféu individual
				var (
					outGold     int64
					outMaterial string
					outRune     string
					outTrophy   *TrophyInstance
					occupies    bool // ocupa a mochila (precisa de espaço / viagem do Pet)
				)
				switch fish.Category {
				case CatMaterial:
					outMaterial, occupies = fish.MaterialID, true
				case CatRune:
					outRune, occupies = fish.RuneID, true
				case CatTrophy:
					w := e.rollWeight(fish, r)
					q, ok := cfg.classifyTrophy(w, fish.MaxWeight)
					if !ok {
						q = QualityCommon // troféu puro garante a faixa mínima
					}
					outTrophy = &TrophyInstance{SpeciesID: fish.SpeciesID, Weight: w, Quality: q, CaughtLocationID: s.Location.ID}
					occupies = true
				default: // CatVendor
					w := e.rollWeight(fish, r)
					if q, ok := cfg.classifyTrophy(w, fish.MaxWeight); ok {
						outTrophy = &TrophyInstance{SpeciesID: fish.SpeciesID, Weight: w, Quality: q, CaughtLocationID: s.Location.ID}
						occupies = true
					} else {
						outGold = fish.GoldValue // tamanho abaixo do limiar → auto-venda
					}
				}

				// Filtro de triagem pode forçar a venda de um item que iria à mochila.
				if occupies && e.filterDecision(s, fish) == FilterSell {
					outGold += fish.GoldValue
					outTrophy, outMaterial, outRune, occupies = nil, "", "", false
				}

				if occupies {
					processHauls(&thauls, &tbp, base+t, s)
					if !e.ensureSpace(s, &t, &thauls, &tbp, base, window, &evStall) {
						goto done // mochila cheia além da janela → defere o evento
					}
				}

				// ===== COMMIT (captura) =====
				res.Events++
				res.Caught++

				wear := cfg.WearPerCatch
				if s.Build.Class == ClassBruiser {
					wear *= cfg.BruiserWearMult
				}
				if weather == WeatherStorm {
					wear *= cfg.StormWearMult
				}
				tdur -= wear
				if tdur <= 0 {
					if s.AutoRepair {
						tdur = s.Build.MaxDurability
						res.RepairsGoldSpent += cfg.RepairGoldCost
					} else {
						tdur = 0
						if !tbroken {
							tbroken = true
							res.BrokeDuringRun = true
							evBroke = true
						}
					}
				}

				e.consumeBait(&tbait)

				if outGold > 0 {
					res.Gold += outGold
					evGold = outGold
				}
				if outTrophy != nil {
					res.Trophies = append(res.Trophies, *outTrophy)
					evTrophy = outTrophy
					tbp++
				}
				if outMaterial != "" {
					res.Materials[outMaterial]++
					evMaterial = outMaterial
					tbp++
				}
				if outRune != "" {
					res.RuneDrops[outRune]++ // o peixe-runa entrega a runa no inventário
					tbp++
				}
				res.XP += fish.XP

				// Pesca Dupla (Trapper / DoubleCatchChance): bônus simplificado.
				if r.Float64() < stats.DoubleCatchChance {
					res.Gold += fish.GoldValue / 4
					res.XP += fish.XP / 2
				}

				// Drops raros.
				if len(cfg.EquipDropTable) > 0 && r.Float64() < cfg.EquipDropChance {
					res.EquipmentDrops = append(res.EquipmentDrops, cfg.EquipDropTable[r.Intn(len(cfg.EquipDropTable))])
				}
				if len(cfg.RuneDropTable) > 0 && r.Float64() < cfg.RuneDropChance {
					res.RuneDrops[cfg.RuneDropTable[r.Intn(len(cfg.RuneDropTable))]]++
				}
			} else {
				// ===== COMMIT (escape) =====
				res.Events++
				res.Escaped++
			}

			if emit != nil {
				ev := GameEvent{
					Index: idx, AtSec: base + t, Weather: string(weather),
					Fish: fish.Name, Category: fish.Category,
				}
				if caught {
					ev.Kind = "catch"
					ev.Gold, ev.Material, ev.Trophy, ev.Broke = evGold, evMaterial, evTrophy, evBroke
				} else {
					ev.Kind = "escape"
				}
				emit(ev)
			}

			res.StalledSeconds += evStall

			// Commit do evento.
			committedT = t
			bp, dur, broken, hauls, bait = tbp, tdur, tbroken, thauls, tbait
			idx++
		}
	}

done:
	s.ElapsedTotal = base + committedT
	s.LastIndex = idx
	s.BackpackCount = bp
	s.Durability = dur
	s.Broken = broken
	s.PetHauls = hauls
	*s.Bait = bait
	return res
}

// ensureSpace espera viagens do Pet até abrir espaço na mochila. Retorna false
// se a próxima viagem necessária ultrapassa a janela (o evento deve ser deferido).
func (e *Engine) ensureSpace(s *Session, t *float64, thauls, tbp *int, base, window float64, evStall *float64) bool {
	for s.BackpackCap > 0 && *tbp >= s.BackpackCap {
		nh := float64(*thauls+1) * s.PetInterval
		if nh-base > window {
			return false
		}
		*evStall += (nh - base) - *t
		*t = nh - base
		*thauls++
		*tbp -= imin(*tbp, s.PetCapacity)
	}
	return true
}

// --- helpers de mecânica ---

// biteTime calcula X efetivo (build/isca reduzem; Trapper reduz; básica penaliza).
func (e *Engine) biteTime(s *Session, b *BaitState) float64 {
	x := s.Location.BaseBiteTime
	speed := s.Build.Stats.BaitSpeed
	if !b.Basic {
		speed += b.Bonus.BaitSpeed
	}
	x = x / (1 + math.Max(speed, 0))
	if s.Build.Class == ClassTrapper {
		x *= 0.85 // Trapper atrai mais rápido
	}
	if b.Basic {
		x *= e.Cfg.BasicBaitBiteMult
	}
	return math.Max(x, 0.1)
}

// effectiveStats soma o bônus da isca (se ativa) e aplica a penalidade de quebra.
func (e *Engine) effectiveStats(s *Session, b *BaitState, broken bool) Stats {
	st := s.Build.Stats
	if !b.Basic && !b.Broken {
		st = addStats(st, b.Bonus)
	}
	if broken {
		st.FishingPower *= e.Cfg.BrokenPowerMult
		st.ReelForce *= e.Cfg.BrokenPowerMult
	}
	return st
}

// WeatherAt expõe o clima determinístico de uma Localização no instante g
// (segundos desde StartTime). Conveniência para clientes/UI.
func (e *Engine) WeatherAt(s *Session, g float64) WeatherType { return e.weatherAt(s, g) }

// weatherAt — clima determinístico: função pura de (WeatherSeed, slot de tempo).
func (e *Engine) weatherAt(s *Session, g float64) WeatherType {
	slot := uint64(g / e.Cfg.WeatherSlotSeconds)
	h := mix64(s.Location.WeatherSeed ^ mix64(slot))
	if int(h%100) < e.Cfg.WeatherStormPct {
		return WeatherStorm
	}
	return WeatherClear
}

// stormForceMult — aumento da Força Exigida na tempestade (Místico ignora parte).
func (e *Engine) stormForceMult(s *Session) float64 {
	extra := e.Cfg.WeatherStormForceMult - 1
	if s.Build.Class == ClassMystic {
		extra *= (1 - e.Cfg.MysticWeatherIgnore)
	}
	return 1 + extra
}

// spawn — pick ponderado, enviesado pela Sorte (proc) e pela tempestade.
func (e *Engine) spawn(s *Session, r *rng, weather WeatherType, stats Stats) *FishTemplate {
	pick := weightedPick(s.Location.SpawnTable, r)
	if r.Float64() < stats.LuckChance {
		tries := 1 + int(stats.LuckPower)
		for i := 0; i < tries; i++ {
			if alt := weightedPick(s.Location.SpawnTable, r); alt.Rarity > pick.Rarity {
				pick = alt
			}
		}
	}
	if weather == WeatherStorm && r.Float64() < 0.5 {
		if alt := weightedPick(s.Location.SpawnTable, r); alt.Rarity > pick.Rarity {
			pick = alt
		}
	}
	return pick
}

func weightedPick(tbl []SpawnEntry, r *rng) *FishTemplate {
	total := 0
	for _, en := range tbl {
		total += en.Weight
	}
	if total <= 0 {
		return tbl[0].Fish
	}
	roll := r.Intn(total)
	for _, en := range tbl {
		roll -= en.Weight
		if roll < 0 {
			return en.Fish
		}
	}
	return tbl[len(tbl)-1].Fish
}

// consumeBait gasta a isca conforme a família.
func (e *Engine) consumeBait(b *BaitState) {
	switch b.Kind {
	case BaitConsumable:
		if b.Basic {
			return
		}
		b.Charges--
		if b.Charges <= 0 {
			if b.StockCharges > 0 {
				refill := imin(b.StockCharges, 500) // lotes de 500 cargas
				b.StockCharges -= refill
				b.Charges = refill
			} else {
				b.Basic = true // sem estoque → cai para a isca básica (não trava)
			}
		}
	case BaitDurable:
		if b.Broken {
			return
		}
		b.Durability--
		if b.Durability <= 0 {
			b.Durability = 0
			b.Broken = true // quebra como equipamento (modo quebrado)
		}
	}
}

// filterDecision aplica as regras de triagem (categoria/raridade/valor).
func (e *Engine) filterDecision(s *Session, fish *FishTemplate) FilterAction {
	for _, f := range s.Filters {
		if f.Category != "" && f.Category != fish.Category {
			continue
		}
		if fish.Rarity < f.MinRarity {
			continue
		}
		if fish.GoldValue < f.MinValue {
			continue
		}
		return f.Action
	}
	return "" // sem regra → padrão (guardar material/troféu)
}

// processHauls aplica todas as viagens do Pet devidas até o tempo global g.
func processHauls(hauls, bp *int, g float64, s *Session) {
	for float64(*hauls+1)*s.PetInterval <= g {
		*hauls++
		*bp -= imin(*bp, s.PetCapacity)
	}
}

func addStats(a, b Stats) Stats {
	return Stats{
		FishingPower:      a.FishingPower + b.FishingPower,
		ReelForce:         a.ReelForce + b.ReelForce,
		LineTension:       a.LineTension + b.LineTension,
		RodHeight:         a.RodHeight + b.RodHeight,
		BaitSpeed:         a.BaitSpeed + b.BaitSpeed,
		DoubleCatchChance: a.DoubleCatchChance + b.DoubleCatchChance,
		LuckChance:        a.LuckChance + b.LuckChance,
		LuckPower:         a.LuckPower + b.LuckPower,
		EscapeReduction:   a.EscapeReduction + b.EscapeReduction,
	}
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
