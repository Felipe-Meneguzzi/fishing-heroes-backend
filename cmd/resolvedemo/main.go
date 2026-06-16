// Demo do motor Resolve: roda uma sessão Idle de 8 horas e imprime o resultado,
// além de demonstrar o determinismo e a aditividade dos lotes.
package main

import (
	"fmt"
	"time"

	"fishingheroes/internal/domain"
)

func main() {
	e := domain.NewEngine(domain.DefaultConfig())

	fmt.Println("=== Fishing Heroes — protótipo do motor Resolve() ===")

	s := buildSession(1337)
	res := e.Resolve(s, 8*3600) // 8 horas de Idle (jogo aberto)
	printRun("Idle de 8h (Brutamontes, isca consumível)", s, res)

	// Determinismo: mesma seed → mesmo resultado.
	a, b := buildSession(99), buildSession(99)
	ra := e.Resolve(a, 4*3600)
	rb := e.Resolve(b, 4*3600)
	fmt.Printf("\n[determinismo] mesma seed produz mesmo ouro/eventos? %v (%d vs %d gold, %d vs %d ev)\n",
		ra.Gold == rb.Gold && ra.Events == rb.Events, ra.Gold, rb.Gold, ra.Events, rb.Events)

	// Aditividade: 1 lote de 6h vs 4 lotes parciais.
	single := buildSession(2024)
	rSingle := e.Resolve(single, 6*3600)

	split := buildSession(2024)
	gSplit, evSplit := int64(0), 0
	for _, until := range []float64{5000, 11111, 18000, 6 * 3600} {
		r := e.Resolve(split, until)
		gSplit += r.Gold
		evSplit += r.Events
	}
	fmt.Printf("[aditividade] 1 lote vs 4 lotes — ouro %d vs %d, eventos %d vs %d, mesmo estado? %v\n",
		rSingle.Gold, gSplit, rSingle.Events, evSplit,
		single.LastIndex == split.LastIndex && single.ElapsedTotal == split.ElapsedTotal)

	// Místico em tempestade ignora parte da penalidade de clima.
	mystic := buildSession(55)
	mystic.Build.Class = domain.ClassMystic
	rm := e.Resolve(mystic, 8*3600)
	printRun("Idle de 8h (Místico)", mystic, rm)
}

func buildSession(seed uint64) *domain.Session {
	vendor := &domain.FishTemplate{ID: "sardine", Name: "Sardinha", Category: domain.CatVendor, Rarity: 0, MinWeight: 0.2, MaxWeight: 1.0, Stamina: 20, Force: 15, GoldValue: 5, XP: 10, SpeciesID: "sardine"}
	material := &domain.FishTemplate{ID: "bonefish", Name: "Peixe-osso", Category: domain.CatMaterial, Rarity: 1, Stamina: 35, Force: 45, XP: 25, MaterialID: "scale"}
	trophy := &domain.FishTemplate{ID: "marlin", Name: "Marlim", Category: domain.CatTrophy, Rarity: 3, MinWeight: 80, MaxWeight: 120, Stamina: 80, Force: 60, XP: 200, SpeciesID: "marlin"}

	loc := &domain.Location{
		ID: "loc1", WorldID: "w1", Level: 1,
		SpawnTable:  []domain.SpawnEntry{{Fish: vendor, Weight: 70}, {Fish: material, Weight: 25}, {Fish: trophy, Weight: 5}},
		WeatherSeed: 0xC0FFEE, BaseBiteTime: 8, GoldPerHour: 1000, XPPerHour: 500,
	}

	return &domain.Session{
		Seed:      seed,
		StartTime: time.Unix(0, 0),
		Build: domain.BuildSnapshot{
			Stats: domain.Stats{FishingPower: 50, ReelForce: 20, LineTension: 30, BaitSpeed: 0.2, LuckChance: 0.15, LuckPower: 1, EscapeReduction: 0.2},
			Class: domain.ClassBruiser, MaxDurability: 100,
		},
		Location:    loc,
		Durability:  100,
		AutoRepair:  true,
		Bait:        &domain.BaitState{Kind: domain.BaitConsumable, Bonus: domain.Stats{BaitSpeed: 0.3}, Charges: 500, StockCharges: 2000},
		BackpackCap: 40,
		PetCapacity: 20,
		PetInterval: 120,
	}
}

func printRun(label string, s *domain.Session, r domain.ResolveResult) {
	fmt.Printf("\n--- %s ---\n", label)
	fmt.Printf("eventos=%d  capturados=%d  escapes=%d\n", r.Events, r.Caught, r.Escaped)
	fmt.Printf("ouro=%d  xp=%d\n", r.Gold, r.XP)
	fmt.Printf("materiais=%v\n", r.Materials)
	fmt.Printf("troféus=%d  drops(equip)=%d  drops(runa)=%v\n", len(r.Trophies), len(r.EquipmentDrops), r.RuneDrops)
	fmt.Printf("reparos(ouro)=%d  quebrou=%v  travado(s)=%.0f\n", r.RepairsGoldSpent, r.BrokeDuringRun, r.StalledSeconds)
	fmt.Printf("estado: idx=%d  resolvido=%.0fs  durabilidade=%.0f  isca_básica=%v\n",
		s.LastIndex, s.ElapsedTotal, s.Durability, s.Bait.Basic)
}
