package domain

import (
	"reflect"
	"testing"
	"time"
)

// --- fixtures ---

func sampleLocation() *Location {
	vendor := &FishTemplate{ID: "sardine", Name: "Sardinha", Category: CatVendor, Rarity: 0, MinWeight: 0.2, MaxWeight: 1.0, Stamina: 20, Force: 15, GoldValue: 5, XP: 10, SpeciesID: "sardine"}
	material := &FishTemplate{ID: "bonefish", Name: "Peixe-osso", Category: CatMaterial, Rarity: 1, Stamina: 35, Force: 45, XP: 25, MaterialID: "scale"}
	trophy := &FishTemplate{ID: "marlin", Name: "Marlim", Category: CatTrophy, Rarity: 3, MinWeight: 80, MaxWeight: 120, Stamina: 80, Force: 60, XP: 200, SpeciesID: "marlin"}
	return &Location{
		ID: "loc1", WorldID: "w1", Level: 1,
		SpawnTable:  []SpawnEntry{{vendor, 70}, {material, 25}, {trophy, 5}},
		WeatherSeed: 0xABCDEF, BaseBiteTime: 8,
		GoldPerHour: 1000, XPPerHour: 500,
	}
}

func newSession(seed uint64) *Session {
	return &Session{
		Seed:      seed,
		StartTime: time.Unix(0, 0),
		Build: BuildSnapshot{
			Stats: Stats{FishingPower: 50, ReelForce: 20, LineTension: 30, BaitSpeed: 0.2, LuckChance: 0.15, LuckPower: 1, EscapeReduction: 0.2},
			Class: ClassBruiser, MaxDurability: 100,
		},
		Location:    sampleLocation(),
		Durability:  100,
		Bait:        &BaitState{Kind: BaitConsumable, Bonus: Stats{BaitSpeed: 0.3}, Charges: 50, StockCharges: 1000},
		BackpackCap: 40,
		PetCapacity: 20,
		PetInterval: 120,
	}
}

// merge soma os deltas de um lote em acc (usado nos testes de aditividade).
func merge(acc *ResolveResult, r ResolveResult) {
	acc.Events += r.Events
	acc.Caught += r.Caught
	acc.Escaped += r.Escaped
	acc.Gold += r.Gold
	acc.XP += r.XP
	acc.RepairsGoldSpent += r.RepairsGoldSpent
	acc.StalledSeconds += r.StalledSeconds
	acc.BrokeDuringRun = acc.BrokeDuringRun || r.BrokeDuringRun
	acc.Trophies = append(acc.Trophies, r.Trophies...)
	acc.EquipmentDrops = append(acc.EquipmentDrops, r.EquipmentDrops...)
	for k, v := range r.Materials {
		acc.Materials[k] += v
	}
	for k, v := range r.RuneDrops {
		acc.RuneDrops[k] += v
	}
}

// --- testes ---

func TestDeterminism(t *testing.T) {
	e := NewEngine(DefaultConfig())
	a, b := newSession(42), newSession(42)

	ra := e.Resolve(a, 3600)
	rb := e.Resolve(b, 3600)

	if !reflect.DeepEqual(ra, rb) {
		t.Fatalf("resultados divergiram para a mesma seed:\n a=%+v\n b=%+v", ra, rb)
	}
	if a.LastIndex != b.LastIndex || a.ElapsedTotal != b.ElapsedTotal {
		t.Fatalf("estado da sessão divergiu: a(idx=%d,t=%.6f) b(idx=%d,t=%.6f)",
			a.LastIndex, a.ElapsedTotal, b.LastIndex, b.ElapsedTotal)
	}
	if ra.Events == 0 {
		t.Fatal("nenhum evento resolvido — fixture inválida")
	}
}

func TestBatchAdditivity(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(*Session)
		splitPoints []float64
	}{
		{"default", func(*Session) {}, []float64{900, 1800.5, 2700, 3600}},
		{"com_stall", func(s *Session) { // pet apertado força travas de mochila
			s.BackpackCap = 5
			s.PetCapacity = 3
			s.PetInterval = 600
		}, []float64{777, 1500, 2222.2, 3000, 3600}},
		{"irregular", func(*Session) {}, []float64{123.4, 456.7, 999.9, 2000, 2000, 3600}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(DefaultConfig())

			single := newSession(7)
			tc.mutate(single)
			rSingle := e.Resolve(single, 3600)

			split := newSession(7)
			tc.mutate(split)
			rSplit := newResult()
			for _, until := range tc.splitPoints {
				merge(&rSplit, e.Resolve(split, until))
			}

			if single.LastIndex != split.LastIndex {
				t.Fatalf("LastIndex divergiu: single=%d split=%d", single.LastIndex, split.LastIndex)
			}
			// ElapsedTotal é float: soma em lotes diferentes diverge por ULPs
			// (soma não-associativa). Comparamos com epsilon — o que importa é o
			// conjunto de eventos resolvidos (LastIndex), que deve bater exato.
			if d := single.ElapsedTotal - split.ElapsedTotal; d < -1e-6 || d > 1e-6 {
				t.Fatalf("ElapsedTotal divergiu além do epsilon: single=%.9f split=%.9f", single.ElapsedTotal, split.ElapsedTotal)
			}
			if single.Durability != split.Durability || single.Broken != split.Broken {
				t.Fatalf("estado de durabilidade divergiu")
			}
			if rSingle.Events != rSplit.Events || rSingle.Caught != rSplit.Caught || rSingle.Escaped != rSplit.Escaped {
				t.Fatalf("contagens divergiram: single(ev=%d,c=%d,e=%d) split(ev=%d,c=%d,e=%d)",
					rSingle.Events, rSingle.Caught, rSingle.Escaped, rSplit.Events, rSplit.Caught, rSplit.Escaped)
			}
			if rSingle.Gold != rSplit.Gold || rSingle.XP != rSplit.XP {
				t.Fatalf("ouro/xp divergiram: single(g=%d,xp=%d) split(g=%d,xp=%d)",
					rSingle.Gold, rSingle.XP, rSplit.Gold, rSplit.XP)
			}
			if !reflect.DeepEqual(rSingle.Materials, rSplit.Materials) {
				t.Fatalf("materiais divergiram: single=%v split=%v", rSingle.Materials, rSplit.Materials)
			}
			if !reflect.DeepEqual(rSingle.Trophies, rSplit.Trophies) {
				t.Fatalf("troféus divergiram: single=%v split=%v", rSingle.Trophies, rSplit.Trophies)
			}
		})
	}
}

func TestMechanicsSmoke(t *testing.T) {
	e := NewEngine(DefaultConfig())
	s := newSession(123)
	s.AutoRepair = true // mantém o equipamento funcional ao longo das 8h
	r := e.Resolve(s, 8*3600)

	t.Logf("caught=%d escaped=%d gold=%d xp=%d mats=%v trophies=%d drops(eq)=%d drops(rune)=%v",
		r.Caught, r.Escaped, r.Gold, r.XP, r.Materials, len(r.Trophies), len(r.EquipmentDrops), r.RuneDrops)

	switch {
	case r.Caught == 0 || r.Escaped == 0:
		t.Fatalf("esperava capturas E escapes; caught=%d escaped=%d", r.Caught, r.Escaped)
	case r.Gold <= 0 || r.XP <= 0:
		t.Fatalf("esperava ouro e xp positivos; gold=%d xp=%d", r.Gold, r.XP)
	case len(r.Materials) == 0:
		t.Fatal("esperava materiais coletados")
	case len(r.Trophies) == 0:
		t.Fatal("esperava troféus capturados (resgate da Sorte em clima limpo)")
	}
}

// Brutamontes sem auto-reparo acaba quebrando o equipamento (e, no início em
// tempestade, isso pode travar a progressão — comportamento emergente esperado).
func TestBreaksWithoutAutoRepair(t *testing.T) {
	e := NewEngine(DefaultConfig())
	s := newSession(123) // AutoRepair = false
	r := e.Resolve(s, 8*3600)
	if !r.BrokeDuringRun || !s.Broken {
		t.Fatalf("esperava equipamento quebrado; broke=%v sessionBroken=%v", r.BrokeDuringRun, s.Broken)
	}
}

func TestAutoRepairSpendsGold(t *testing.T) {
	e := NewEngine(DefaultConfig())
	s := newSession(123)
	s.AutoRepair = true
	r := e.Resolve(s, 8*3600)

	if r.RepairsGoldSpent <= 0 {
		t.Fatal("auto-reparo deveria ter gasto ouro")
	}
	if s.Broken {
		t.Fatal("com auto-reparo o equipamento não deveria ficar quebrado")
	}
}

func TestBaitFallsBackToBasic(t *testing.T) {
	e := NewEngine(DefaultConfig())
	s := newSession(123)
	s.Bait = &BaitState{Kind: BaitConsumable, Bonus: Stats{BaitSpeed: 0.3}, Charges: 10, StockCharges: 0}
	e.Resolve(s, 8*3600)

	if !s.Bait.Basic {
		t.Fatal("isca consumível sem estoque deveria cair para básica")
	}
}

func TestWeatherDeterministic(t *testing.T) {
	e := NewEngine(DefaultConfig())
	s := newSession(1)
	// Mesmo instante → mesmo clima, sempre.
	for _, g := range []float64{0, 1800, 3600, 10000, 28800} {
		if e.weatherAt(s, g) != e.weatherAt(s, g) {
			t.Fatalf("clima não-determinístico em g=%.0f", g)
		}
	}
}
