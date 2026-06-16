// Package domain contém o núcleo de regras puras do Fishing Heroes RPG.
//
// O coração é o motor Resolve() (resolve.go): a resolução determinística e
// preguiçosa (lazy) da pesca comum no estado Idle (jogo aberto). Tudo é função
// pura de (build, localização, seed, índice do evento) — ver ARCHITECTURE.md.
package domain

import "time"

// ClassType — especialização do pescador.
type ClassType string

const (
	ClassBruiser ClassType = "bruiser" // +força bruta, +desgaste de durabilidade
	ClassMystic  ClassType = "mystic"  // +raridade (Sorte), ignora parte do clima
	ClassTrapper ClassType = "trapper" // +velocidade de atração, +pesca dupla
)

// Stats — atributos efetivos (já somados: base + classe + equip + runas + skill + aquário).
type Stats struct {
	FishingPower      float64
	ReelForce         float64
	LineTension       float64
	RodHeight         float64
	BaitSpeed         float64 // reduz X (intervalo de mordida)
	DoubleCatchChance float64 // Trapper / runas
	LuckChance        float64 // probabilidade de proc da Sorte (resgate e spawn)
	LuckPower         float64 // magnitude da Sorte
	EscapeReduction   float64 // reduz o tempo "preso" no escape (base 50%)
}

// QualityTier — faixa de qualidade de um troféu (escala o bônus do Aquário).
//
// As faixas são derivadas do tamanho sorteado na captura como fração do peso
// MÁXIMO da espécie (ver Engine.classifyTrophy / BalanceConfig.Trophy*Pct):
// ≥80% Comum, ≥85% Raro, ≥90% Épico, ≥95% Lendário, 100% Perfeito. Abaixo de
// 80% o peixe NÃO vira troféu: é auto-vendido por ouro.
type QualityTier string

const (
	QualityCommon    QualityTier = "common"
	QualityRare      QualityTier = "rare"
	QualityEpic      QualityTier = "epic"
	QualityLegendary QualityTier = "legendary"
	QualityPerfect   QualityTier = "perfect"
)

// FishCategory — destino do peixe na resolução.
const (
	CatVendor   = "vendor"   // venda por ouro; vira troféu se o tamanho sorteado ≥80% do máximo
	CatMaterial = "material" // contagem fungível (peixe de crafting)
	CatTrophy   = "trophy"   // sempre instância individual (peso/qualidade)
	CatRune     = "rune"     // concede uma runa (peixe-runa)
	CatBoss     = "boss"     // tratado pela Batalha de Boss (fora do Resolve)
)

// FishTemplate — definição estática de uma espécie (read-only, vinda do banco/RAM).
type FishTemplate struct {
	ID         string
	Name       string
	Category   string
	Rarity     int     // 0 = comum; maior = mais raro
	MinWeight  float64 // tamanho mínimo sorteável (kg)
	MaxWeight  float64 // tamanho máximo sorteável; base das faixas de troféu (% do máximo)
	Stamina    float64 // determina a DURAÇÃO da luta (÷ poder)
	Force      float64 // Força Exigida: captura se poder >= Force
	GoldValue  int64   // valor de venda (vendor, quando não vira troféu)
	XP         int64   // XP concedido ao capturar
	MaterialID string  // id do material concedido (categoria material)
	RuneID     string  // id do template de runa concedido (categoria rune)
	SpeciesID  string  // id da espécie p/ o troféu/Aquário (normalmente = ID)
}

// SpawnEntry — peso de uma espécie na roleta de spawn da Localização.
type SpawnEntry struct {
	Fish   *FishTemplate
	Weight int
}

// WeatherType — clima de uma Localização (função determinística do tempo).
type WeatherType string

const (
	WeatherClear WeatherType = "clear"
	WeatherStorm WeatherType = "storm"
)

// Location — onde se pesca. Spawn, clima e baseline do modo desligado.
type Location struct {
	ID           string
	WorldID      string
	Name         string // ex.: "Campos"
	Level        int
	SpawnTable   []SpawnEntry
	WeatherSeed  uint64
	BaseBiteTime float64 // X base (segundos) antes dos modificadores de build
	GoldPerHour  int64   // baseline do modo desligado
	XPPerHour    int64
}

// BaitKind — família da isca.
type BaitKind string

const (
	BaitConsumable BaitKind = "consumable" // cargas gastas por peixe
	BaitDurable    BaitKind = "durable"    // uso infinito; quebra e conserta
	BaitBoss       BaitKind = "boss"       // 1 carga por Batalha de Boss
)

// BaitState — estado da isca equipada dentro de uma sessão.
type BaitState struct {
	Kind         BaitKind
	Bonus        Stats   // bônus aplicado enquanto a isca está ativa (e não básica)
	Charges      int     // consumível: carga atual em uso
	StockCharges int     // consumível: cargas no estoque (auto-recarga)
	Durability   float64 // durável: durabilidade atual
	MaxDur       float64 // durável: durabilidade máxima
	Broken       bool    // durável: em modo quebrado
	Basic        bool    // caiu para isca básica (sem bônus)
}

// FilterAction — decisão de triagem.
type FilterAction string

const (
	FilterSell FilterAction = "sell"
	FilterKeep FilterAction = "keep"
)

// FilterRule — regra de triagem por categoria/raridade/valor mínimo.
type FilterRule struct {
	Category  string // "" = qualquer
	MinRarity int    // 0 = qualquer
	MinValue  int64  // ouro mínimo
	Action    FilterAction
}

// BuildSnapshot — build congelada no início da sessão.
type BuildSnapshot struct {
	Stats         Stats
	Class         ClassType
	MaxDurability float64
}

// TrophyInstance — troféu individual capturado (peso/qualidade sorteados).
type TrophyInstance struct {
	SpeciesID        string      `json:"speciesId"`
	Weight           float64     `json:"weight"`           // peso sorteado na captura
	Quality          QualityTier `json:"quality"`          // faixa que escala o bônus do Aquário
	CaughtLocationID string      `json:"caughtLocationId"` // local de captura (guardado no stash)
}

// Session — estado vivo de uma sessão de pesca. Os campos mutáveis avançam a
// cada Resolve()/claim. Equivale à linha fishing_session do Postgres.
type Session struct {
	Seed      uint64
	StartTime time.Time
	Build     BuildSnapshot
	Location  *Location

	// --- estado mutável (avança a cada claim) ---
	LastIndex    int
	ElapsedTotal float64 // segundos desde StartTime já resolvidos (âncora)

	Durability float64
	Broken     bool
	AutoRepair bool

	Bait *BaitState

	BackpackCount int
	BackpackCap   int

	PetCapacity int
	PetInterval float64 // segundos por viagem
	PetHauls    int     // viagens já realizadas

	Filters []FilterRule
}
