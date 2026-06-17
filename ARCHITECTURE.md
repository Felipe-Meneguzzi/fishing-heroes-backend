# Arquitetura de Sistema: Idle Fishing RPG

Este documento descreve a arquitetura técnica para um jogo Idle RPG de Pesca multiplayer, focado em alta escalabilidade, segurança contra fraudes (*Server Authoritative*), simulação de progresso offline, e sistemas de progressão profunda (Runas, Classes, Alquimia e Aquário).

---

## 1. Visão Geral da Stack

* **Backend (Servidor)**: Golang (Go) - Alta performance, concorrência com Goroutines e baixo uso de recursos por sessão.
* **Cliente (Engine)**: Godot 4.x - Integração nativa com WebSockets e renderização assíncrona baseada em eventos (Signals).
* **Banco de Dados Principal**: PostgreSQL - Armazenamento persistente de contas, inventários, equipamentos, Runas e dados do Aquário.
* **Cache em Memória**: Redis - cache quente das sessões em foreground e publicação do clima atual por Localização (`Loc:{id}:Weather`). Não é fonte da verdade.

---

## 2. Filosofia Central: Desacoplamento e Autoridade do Servidor

* **Server-Authoritative**: O cliente Godot é apenas um visualizador. Toda decisão matemática (clima, spawn, sucesso da captura, rolls de crafting, itens gerados) ocorre estritamente no backend.
* **Resolução Preguiçosa (Lazy) e Determinística**: a pesca comum **não** roda tick a tick no servidor. O resultado é função pura de `(build, zona, seed, índice do evento)`; o servidor só executa `Resolve()` quando o cliente pede (retorno do offline ou lote online). Resolver 8h de pescaria custa ~0,6 ms de CPU.
* **Dois estados passivos**: (1) **Idle (jogo aberto)** — o foco — roda o `Resolve()` lazy completo, gerando peixes/troféus/drops/XP a partir da linha `fishing_session`. (2) **Jogo desligado (app fechado)** — recompensa de catch-up por **fórmula fechada** (ouro+XP médios da melhor Localização alcançada × (1 − X%), até 8h), sem simular eventos e sem precisar da seed/sessão.
* **Jogador parado = custo ~zero**: ninguém consome CPU/goroutine/timer enquanto não pede `Resolve()` ou `claim`. O estado desligado é um único cálculo O(1) no login.
* **Persistência Enxuta**: nada é escrito por tick. O Postgres é atualizado apenas no `claim`/fim de lote e em ações de menu (craft, alquimia, oferenda). Como o offline é recalculado da seed, um crash do servidor não perde progresso offline.

---

## 3. Arquitetura do Servidor (Golang)

### 3.1. Camada de Domínio (Core Domain)
Contém as regras matemáticas puras: o **motor de resolução determinística** da pesca comum (intervalo de mordida, spawn, captura, luta, Sorte) e as fórmulas que consideram a `ClassType` e os modificadores globais do `Aquarium`. O combate com `StatusEffects` (sangramento, fúria) pertence ao motor da **Batalha de Boss** (a definir).

### 3.2. Casos de Uso (Use Cases)
Orquestram os fluxos de regras de negócio:
* `StartFishingSession()`: congela a build num snapshot, sorteia a seed, avalia o Clima da Zona e cria a linha `fishing_session`.
* `ResolveSession()`: motor lazy — resolve todos os eventos desde `last_time` (offline ou lote online), agrega recompensas/XP/drops e persiste numa transação.
* `CraftEquipment()` / `ReforgeEquipment()`: fabrica/re-rola equipamento com rolls **server-seeded** consumindo materiais/ouro.
* `SetRune()`: engasta/troca runa (reversível e seguro), com custo de ouro opcional.
* `CraftAlchemyBait()`: consome materiais para gerar iscas (consumíveis em lote ou duráveis).
* `OfferToAquarium()`: registra um troféu, mantém o melhor por espécie e recalcula os buffs globais.
* `StartBossBattle()` / `SubmitBossResult()`: inicia a batalha ativa (consome a isca de boss, sorteia o seed) e valida o resultado por **replay determinístico** do log de inputs.

### 3.3. Delivery Layer (Comunicação)
* **HTTP REST API (fluxo principal)**: autenticação, `claim` de progresso, configurações do menu, trocas de Classe, Engastes de Runas, Alquimia, Aquário e início/fim de sessão de pesca.
* **WebSockets (opcional, só em foreground)**: quando o app está aberto assistindo, o cliente recebe os eventos resolvidos para o Godot animar. Não é obrigatório para o progresso — mantém o servidor *stateless* e a escala horizontal trivial. A animação tick a tick é **cosmética**, reproduzida no cliente a partir do resultado + seed enviados pelo servidor.

### 3.4. Motor de Pesca e Eventos Globais
* **Resolução por Eventos (lazy, sem timers por jogador)**: não há `Bite Timer` nem worker varrendo jogadores. A próxima mordida, as capturas e as viagens do Pet são **calculadas dentro de `Resolve()`** a partir do tempo decorrido e da seed. O Pet é um *hauler temporizado* simulado no loop, não um worker fazendo polling.
* **Clima determinístico (sem worker autoritativo)**: o clima é uma **função pura do tempo** por Localização — `clima(localização, t) = hash(seedGlobal, localização, slot)`. O `Resolve()` o reconstrói em qualquer instante, sem histórico. Um worker leve apenas **publica** o clima atual no Redis (`Loc:{id}:Weather`) para a UI/notificação — não é a fonte da verdade.

---

## 4. O Padrão Controller-UseCase-Repository

### Controllers
* **REST HTTP Handlers (fluxo principal)**: `claim`/resolução de sessão, builds (troca de runas/classe/isca) e requisições do Porto (Crafting, Re-forja, Alquimia, Aquário).
* **WebSocket Router (opcional)**: só em foreground; faz streaming dos eventos já resolvidos para a animação no Godot.
* **Worker leve**: publica o clima atual de cada Localização no Redis para a UI (o clima em si é determinístico). Sem timers por jogador.

### Repositories
* **Redis Repository**: publicação do clima atual por Localização (`Loc:{id}:Weather`, com TTL — apenas cache pra UI, já que o clima é determinístico) e cache **quente** das sessões em foreground. Não é fonte da verdade — pode ser perdido sem perda de progresso.
* **PostgreSQL Repository**: fonte da verdade. Jogador, Aquário, Runas engastadas, inventários e a linha `fishing_session` (seed, snapshot da build, `last_index`, `last_time`).

---

## 5. Fluxos de Exemplo

### A. Fluxo de Pesca Comum — Estado Idle, jogo aberto (Resolução por Eventos)
Roda enquanto o jogo está **aberto** (o foco), resolvendo o tempo real decorrido em lotes. É o **único** fluxo que gera peixes, troféus, materiais e drops.

1. **Carrega a sessão**: lê a linha `fishing_session` (seed, snapshot da build, zona, isca, `last_index`, `last_time`).
2. **Calcula a janela**: `elapsed = agora − last_time` (o tempo real desde o último lote).
3. **Loop por evento** (determinístico, ~µs por evento):
   * Deriva o RNG de `hash(seed, índice)`.
   * Sorteia o **intervalo de mordida** ∈ [X, 3X] (X modificado por classe/build).
   * Sorteia o **spawn** (raridade enviesada pela Sorte).
   * **Resolve a captura**: `poder ≥ exigido` → captura; senão a Sorte (Chance/Power) pode resgatar.
   * Aplica **duração da luta** (∝ estamina ÷ poder), **desgaste de durabilidade** (equip + isca durável) e o **escape** (50% do tempo, redutível).
   * Em caso de captura: concede **ouro/material/troféu + XP**, consome **1 carga** da isca consumível (ou durabilidade da durável) e rola um eventual **drop raro** de equipamento/runa.
   * Atualiza **mochila/Pet/Stash**; se a mochila enche, a pesca **trava** até a próxima viagem do Pet. O acúmulo de XP pode disparar **level-ups** → pontos de Skill Tree.
4. **Agrega e persiste**: acumula ouro, materiais (contagem), troféus (instances) e novas espécies; grava numa transação e atualiza `last_index`/`last_time`.
5. **(Online) Anima**: envia o resultado + seed via WebSocket para o Godot reproduzir a animação localmente.

> **Anti-cheat**: o `elapsed` é limitado pelo relógio do servidor e a seed nunca sai dele, então o cliente não consegue acelerar o tempo nem fabricar peixes.

### B. Recompensa de Jogo Desligado (Fórmula Fechada)
Calculada uma única vez no **login**. **Não usa a seed nem simula eventos.**
1. `horas = min(login − logout, 8h)`.
2. `ouro += horas · ouroPorHora(melhorLocalização) · (1 − X)`.
3. `xp += horas · xpPorHora(melhorLocalização) · (1 − X)` — pode disparar level-ups.
4. `X` (redução) diminui com upgrades. **Não** gera peixes, troféus, materiais nem drops.

Custo: O(1) por jogador, no login. Zero estado vivo.

### C. Batalha de Boss (Ativa — Cliente Simula, Servidor Revalida)
Único fluxo **ativo/real-time**, mas barato: só foreground, iniciado pelo jogador, curto e raro. **Não** ticka no servidor — a autoridade vem de um **replay determinístico**.

1. **`StartBossBattle(tier)`**: o servidor valida a isca de boss (tier T) do Mundo atual, **consome 1 carga** (perder = isca gasta), sorteia o `seed` da luta, congela o `buildSnapshot` e devolve `{seed, bossParams(T), buildSnapshot}`. Grava um registro `boss_battle` pendente.
2. **Cliente simula localmente**: roda o **motor determinístico de luta** (idêntico ao do servidor) com esses params — feel perfeito, sem latência. O jogador joga (recolher × soltar), acumulando um **log de inputs** (timeline) e o resultado pretendido.
3. **`SubmitBossResult(inputLog)`**: o servidor **re-simula** o motor com `seed` + `buildSnapshot` + `bossParams` + `inputLog` → calcula o **resultado autoritativo**. Valida plausibilidade (taxa de inputs limitada, timestamps monotônicos, tempos de reação humanos, duração coerente).
4. **Resolução autoritativa**: vitória → recompensas numa transação (1ª vez no Mundo desbloqueia o próximo; re-batalhas dão drops escalados pelo tier). Derrota → a isca já foi consumida; fecha o registro.

> **Anti-cheat:** suficiente para PvE (a fraude afeta sobretudo a própria conta; o custo da isca limita spam). Risco residual: como o cliente conhece o seed, um cliente modificado poderia forjar um log "perfeito mas plausível". Mitigações: checagens estatísticas de reação/variância e cap de inputs. **Hardening futuro:** migrar bosses de tier alto / rankings para o modelo *servidor roda a luta* (autoritativo por tick).

> **Custo:** 1 replay determinístico por submissão (poucos ms). Sem estado vivo, sem tick por jogador.

---

## 6. Modelo de Domínio Expandido (Go)

Abaixo as estruturas que representam a integração das novas mecânicas: Classes, Runas, Clima, Status e Aquário.

### 6.1. Atributos Básicos e Classes
```go
package domain

type ClassType string

const (
	ClassBruiser ClassType = "bruiser" // +Força bruta, +Desgaste de durabilidade
	ClassMystic  ClassType = "mystic"  // Foco em Raridade, ignora penalidades de clima
	ClassTrapper ClassType = "trapper" // Foco em Velocidade de Atração e Pesca Dupla
)

type Stats struct {
	FishingPower      float64
	ReelForce         float64
	LineTension       float64
	RodHeight         float64
	BaitSpeed         float64 // reduz X (intervalo de mordida)
	DoubleCatchChance float64 // Trapper / Runas específicas
	LuckChance        float64 // probabilidade de proc da Sorte (resgate e spawn)
	LuckPower         float64 // magnitude da Sorte (+Y de força no resgate; intensidade do upgrade de raridade)
	EscapeReduction   float64 // reduz o tempo "preso" no escape (base 50%)
}
```

### 6.2. Equipamentos e Runas (Inscrições)
```go
type FishStatusEffect string

const (
	EffectBleed     FishStatusEffect = "bleed"
	EffectExhausted FishStatusEffect = "exhausted"
)

type Rune struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	BonusStats  Stats            `json:"bonus_stats"`
	ApplyStatus FishStatusEffect `json:"apply_status"` // Status ativo que a runa causa no peixe
}

type Equipment struct {
	InstanceID    string    `json:"instance_id"`
	Type          string    `json:"type"` // rod, reel, line
	BonusStats    Stats     `json:"bonus_stats"` // rolagem server-seeded (crafting híbrido)
	MaxDurability float64   `json:"max_durability"`
	Durability    float64   `json:"durability"`
	Runes         []*Rune   `json:"runes"` // Slots de engaste (engaste reversível e seguro)
}

type BaitKind string

const (
	BaitConsumable BaitKind = "consumable" // cargas gastas por peixe (craft em lote, ex.: 500)
	BaitDurable    BaitKind = "durable"    // uso infinito; quebra e conserta como equipamento
	BaitBoss       BaitKind = "boss"       // 1 carga por Batalha de Boss
)

type Bait struct {
	ID         string   `json:"id"`
	Kind       BaitKind `json:"kind"`
	Tier       int      `json:"tier"`        // bosses: define dificuldade e tier de recompensa
	BonusStats Stats    `json:"bonus_stats"` // ex.: viés de spawn, BaitSpeed
	Charges    int      `json:"charges"`     // consumíveis / boss
	Durability float64  `json:"durability"`  // duráveis
}
```

### 6.3. Peixes, Status Effects e Clima Global
```go
type FishState string

const (
	FishStateEnraged FishState = "enraged" // Ativado randomicamente pelo peixe na luta
)

type Fish struct {
	TemplateID string  `json:"template_id"`
	Name       string  `json:"name"`
	Category   string  `json:"category"`   // vendor (venda/troféu), material, rune, boss, trophy
	MinWeight  float64 `json:"min_weight"` // tamanho mínimo sorteável
	MaxWeight  float64 `json:"max_weight"` // tamanho máximo; base das faixas de troféu (% do máx)
	Stamina    float64 `json:"stamina"`    // determina a DURAÇÃO da luta (÷ poder do jogador)
	Force      float64 `json:"force"`      // Força Exigida: captura se poder >= Force (senão a Sorte pode resgatar)
}
// vendor: o servidor sorteia o peso por uma Normal (Gauss) centrada no tamanho médio,
// clampada a [min,max]; ≥80% do máximo vira troféu, senão auto-venda.

type WeatherType string

const (
	WeatherClear WeatherType = "clear"
	WeatherStorm WeatherType = "storm" // Aumenta força de fuga dos peixes, mas viabiliza Bosses
)

// Hierarquia de conteúdo: Mundo › Localização.
type Location struct {
	ID          string `json:"id"`
	WorldID     string `json:"world_id"`
	Level       int    `json:"level"`
	SpawnTable  string `json:"spawn_table"`  // tabela de peixes da Localização
	WeatherSeed uint64 `json:"weather_seed"` // base do clima determinístico desta Localização
}

type World struct {
	ID        string      `json:"id"`
	Order     int         `json:"order"`        // progressão linear
	Locations []*Location `json:"locations"`
	ActBossID string      `json:"act_boss_id"`  // pescável em qualquer Localização do Mundo
}
```

### 6.4. Aquário Monumental (Progressão Global)
```go
type AquariumDisplay struct {
	FishTemplateID string      `json:"fish_template_id"`
	Quality        QualityTier `json:"quality"`       // melhor faixa já ofertada desta espécie
	BonusGranted   Stats       `json:"bonus_granted"` // buff global, escalado pela faixa de qualidade
}

type Aquarium struct {
	DisplayedFish map[string]AquariumDisplay `json:"displayed_fish"`
}
```

### 6.5. Entidade do Jogador Consolidada
```go
type Player struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Class       ClassType `json:"class"`
	BaseStats   Stats     `json:"base_stats"`

	// Economia e Progressão
	Gold        int64 `json:"gold"`
	Level       int   `json:"level"`
	XP          int64 `json:"xp"`
	SkillPoints int   `json:"skill_points"`

	// Build Atual
	EquippedRod  *Equipment `json:"equipped_rod"`
	EquippedReel *Equipment `json:"equipped_reel"`
	EquippedLine *Equipment `json:"equipped_line"`
	ActiveBaitID string     `json:"active_bait_id"` // isca equipada (consumível/durável/boss)

	// Inventário (Stash): comuns viram ouro; materiais = contagens; troféus = instâncias
	Materials   map[string]int    `json:"materials"`
	Trophies    []*TrophyInstance `json:"trophies"`
	ActivePetID string            `json:"active_pet_id"` // pets colecionáveis; um ativo por vez
	SkillTree   SkillTree         `json:"skill_tree"`
	Aquarium    Aquarium          `json:"aquarium"` // progressão global passiva
	Filters     []FilterRule      `json:"filters"`  // triagem: categoria/raridade/valor
}

// CalculateTotalStats aplica Base + Classe + Equipamentos + Runas + SkillTree + Aquário
func (p *Player) CalculateTotalStats() Stats {
	total := p.BaseStats

	// 1. Aplicar multiplicadores da Classe
	if p.Class == ClassBruiser {
		total.FishingPower *= 1.20 // +20% força
	} else if p.Class == ClassTrapper {
		total.DoubleCatchChance += 0.05
	} else if p.Class == ClassMystic {
		total.LuckChance += 0.05
		total.LuckPower += 0.05
		// + ignora parcialmente penalidades de clima (aplicado na resolução)
	}

	// 2. Somar Equipamentos e Runas engastadas
	equips := []*Equipment{p.EquippedRod, p.EquippedReel, p.EquippedLine}
	for _, eq := range equips {
		if eq != nil {
			total.FishingPower += eq.BonusStats.FishingPower
			// ... soma demais atributos base do equipamento
			
			// Soma bônus das Runas daquele equipamento
			for _, rune := range eq.Runes {
				total.FishingPower += rune.BonusStats.FishingPower
				total.LineTension += rune.BonusStats.LineTension
				// ...
			}
		}
	}

	// 3. Somar buffs globais do Aquário Monumental
	for _, display := range p.Aquarium.DisplayedFish {
		total.FishingPower += display.BonusGranted.FishingPower
		total.LineTension += display.BonusGranted.LineTension
		// ...
	}

	// 4. Aplicar multiplicadores da Skill Tree
	// ... (ex: +% global de Fishing Power)
	
	return total
}
```

### 6.6. Sorte, Troféus e Sessão

```go
type QualityTier string

// Faixas derivadas do peso sorteado como % do tamanho MÁXIMO da espécie:
// ≥80% Comum, ≥85% Raro, ≥90% Épico, ≥95% Lendário, 100% Perfeito.
// Abaixo de 80% o peixe não vira troféu (auto-venda por ouro).
const (
	QualityCommon    QualityTier = "common"
	QualityRare      QualityTier = "rare"
	QualityEpic      QualityTier = "epic"
	QualityLegendary QualityTier = "legendary"
	QualityPerfect   QualityTier = "perfect"
)

// Troféu: única categoria de peixe guardada como instância individual.
type TrophyInstance struct {
	InstanceID       string      `json:"instance_id"`
	SpeciesID        string      `json:"species_id"`
	Weight           float64     `json:"weight"`             // sorteado na captura
	Quality          QualityTier `json:"quality"`            // faixa que escala o bônus no Aquário
	CaughtLocationID string      `json:"caught_location_id"` // local de captura (guardado no Stash)
}

// Estado persistido da sessão ativa (1 linha por jogador). Reconstrói o offline.
type FishingSession struct {
	PlayerID        string    `json:"player_id"`
	Seed            uint64    `json:"seed"`              // guardado SÓ no servidor
	StartTime       time.Time `json:"start_time"`
	LocationID      string    `json:"location_id"`
	BaitID          string    `json:"bait_id"`
	BaitChargesLeft int       `json:"bait_charges_left"` // isca consumível (depleta na sessão)
	BaitDurability  float64   `json:"bait_durability"`   // isca durável
	BuildSnapshot   Stats     `json:"build_snapshot"`    // congelado no início da sessão
	LastIndex       int       `json:"last_index"`        // último evento resolvido
	LastTime        time.Time `json:"last_time"`         // âncora anti-cheat
	BackpackCount   int       `json:"backpack_count"`
	Durability      float64   `json:"durability"`
	Broken          bool      `json:"broken"`            // modo quebrado (poder reduzido)
	AutoRepair      bool      `json:"auto_repair"`
}
```

### 6.7. Batalha de Boss

```go
type BossTemplate struct {
	ID          string  `json:"id"`
	WorldID     string  `json:"world_id"`
	Name        string  `json:"name"`
	BaseStamina float64 `json:"base_stamina"` // escala com o tier da isca
	BaseForce   float64 `json:"base_force"`   // base da tensão por tick ao recolher
	EnrageEvery float64 `json:"enrage_every"` // intervalo-base dos ciclos de fúria (seedado)
	EnrageMult  float64 `json:"enrage_mult"`  // multiplicador de força durante a fúria
}

// Um evento no log de inputs do cliente (timeline recolher × soltar).
type InputEvent struct {
	T       float64 `json:"t"`       // segundos desde o início da luta
	Reeling bool    `json:"reeling"` // true = recolhendo; false = soltou
}

// Registro pendente/concluído da batalha (não toca em fishing_session).
type BossBattle struct {
	ID            string    `json:"id"`
	PlayerID      string    `json:"player_id"`
	BossID        string    `json:"boss_id"`
	Tier          int       `json:"tier"`
	Seed          uint64    `json:"seed"`           // governa fúria/exaustão; guardado no servidor
	BuildSnapshot Stats     `json:"build_snapshot"`
	StartTime     time.Time `json:"start_time"`
	Status        string    `json:"status"` // in_progress, won, lost
}
```

### 6.8. Pet, Skill Tree e Filtros

```go
type PetTemplate struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	BaseCapacity int      `json:"base_capacity"` // itens por viagem
	BaseInterval float64  `json:"base_interval"` // segundos por viagem
	Traits       []string `json:"traits"`        // perks/características (cosmético + funcional)
}
// Pets são colecionáveis (um ativo por vez). A Skill Tree aplica multiplicadores
// GLOBAIS que afetam todos os pets (ex.: +5% velocidade) → capacidade/intervalo
// efetivos = base do template × multiplicadores globais.

type SkillTree struct {
	Nodes map[string]int `json:"nodes"` // nodeID -> rank investido
}

type FilterAction string

const (
	FilterSell FilterAction = "sell"
	FilterKeep FilterAction = "keep"
)

// Regra de triagem: casa por categoria/raridade/valor e decide vender × guardar.
type FilterRule struct {
	Category  string       `json:"category"`   // "" = qualquer
	MinRarity int          `json:"min_rarity"` // 0 = qualquer
	MinValue  int64        `json:"min_value"`  // ouro mínimo
	Action    FilterAction `json:"action"`
}
```

---

## 7. Modelo de Dados e Persistência

### 7.1. Fonte da Verdade (PostgreSQL)
**Estado do jogador (UUID):**
* `players` — conta, classe, `base_stats`, ouro, nível/XP, skill tree (JSONB), filtros (JSONB), pet ativo, melhor Localização.
* `player_equipment` — instâncias de vara/molinete/linha (stats rolados, durabilidade, slot equipado).
* `equipment_runes` — runa (template) engastada em cada slot de um equipamento.
* `player_runes` — inventário de runas não-engastadas (contagem por template).
* `player_materials` — contagens fungíveis por material.
* `player_baits` — iscas do jogador (cargas/durabilidade).
* `player_trophies` — troféus individuais (peso, qualidade).
* `aquarium` — melhor troféu por espécie + bônus cacheado.
* `player_boss_clears` — progressão: melhor tier derrotado por boss/mundo.
* `player_pets` — pets colecionáveis do jogador (um ativo por vez).

**Sessão e batalha:**
* `fishing_session` — uma linha por jogador (ver `FishingSession`).
* `boss_battle` — registro pendente/concluído de batalha.

**Templates (seed + admin, IDs em texto):** `worlds`, `locations`, `spawn_tables`, `fish_templates`, `material_templates`, `rune_templates`, `equipment_templates`, `recipes`, `boss_templates`, `pet_templates`. Read-mostly; carregados na RAM no boot.

### 7.2. Estado Quente / Efêmero (Redis)
* `Loc:{id}:Weather` — publicação do clima atual por Localização, com TTL (apenas cache pra UI; o clima é determinístico).
* `sess:{playerId}` — **cache quente da sessão** (âncora + baseline ouro/XP), TTL ~2h. O caminho QUENTE (tick do WebSocket / preview) lê daqui em velocidade de memória, sem bater no Postgres a cada ~1,5s; o claim atualiza Redis + Postgres. Implementado em `internal/repo/cache.go`.
* Pode ser perdido sem perda de progresso (Postgres é a verdade): em miss, repovoa do Postgres.

### 7.3. Templates (RAM)
Peixes, Mundos/Localizações, tabelas de spawn, runas e receitas são read-only e carregados em memória no boot.

### 7.4. Cadência de Escrita
* **Zero escrita por tick.**
* `claim`/fim de lote: uma transação aplica os deltas (ouro +=, materiais +=, insere troféus, atualiza Aquário) e atualiza `last_index`/`last_time`.
* Ações de menu (craft, alquimia, engaste, oferenda): transacionais e imediatas.
* **Gargalo real do sistema = TPS/conexões do Postgres** (não a CPU do Go). Por isso a escrita é enxuta e em lote.

### 7.5. Schema Detalhado (DDL)

Convenções: instâncias usam `UUID`; templates usam `TEXT` legível. Stats e snapshots em `JSONB`. Seeds `uint64` guardadas como `BIGINT` (bit-preserving). Timestamps em `TIMESTAMPTZ`.

```sql
-- ========== TEMPLATES (seed + admin; read-mostly, carregados na RAM no boot) ==========
CREATE TABLE worlds (
    id          TEXT PRIMARY KEY,
    ordering    INT  NOT NULL,                       -- progressão linear
    act_boss_id TEXT NOT NULL
);

CREATE TABLE locations (
    id             TEXT PRIMARY KEY,
    world_id       TEXT NOT NULL REFERENCES worlds(id),
    level          INT  NOT NULL,
    spawn_table_id TEXT NOT NULL,
    weather_seed   BIGINT NOT NULL,
    gold_per_hour  BIGINT NOT NULL,                  -- baseline do modo desligado
    xp_per_hour    BIGINT NOT NULL
);

CREATE TABLE fish_templates (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    category         TEXT NOT NULL,                   -- vendor, material, rune, boss, trophy
    rarity           INT  NOT NULL DEFAULT 0,         -- viés da Sorte no spawn / min_rarity dos filtros
    min_weight       REAL NOT NULL DEFAULT 0,         -- tamanho mínimo sorteável
    max_weight       REAL NOT NULL DEFAULT 0,         -- tamanho máximo; base das faixas de troféu
    stamina          REAL NOT NULL,                   -- duração da luta
    force            REAL NOT NULL,                   -- Força Exigida
    gold_value       BIGINT NOT NULL DEFAULT 0,
    xp               BIGINT NOT NULL DEFAULT 0,
    material_id      TEXT,                            -- categoria material
    rune_template_id TEXT,                            -- categoria rune
    species_id       TEXT                             -- espécie p/ troféu/Aquário (default = id)
);

CREATE TABLE spawn_tables (
    spawn_table_id   TEXT NOT NULL,
    fish_template_id TEXT NOT NULL REFERENCES fish_templates(id),
    weight           INT  NOT NULL,                   -- peso na roleta de spawn
    PRIMARY KEY (spawn_table_id, fish_template_id)
);

CREATE TABLE material_templates ( id TEXT PRIMARY KEY, name TEXT NOT NULL );

CREATE TABLE rune_templates (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    bonus_stats  JSONB NOT NULL,
    apply_status TEXT                                 -- bleed, etc. (NULL se nenhum)
);

CREATE TABLE equipment_templates (
    id             TEXT PRIMARY KEY,
    type           TEXT NOT NULL,                     -- rod, reel, line
    roll_ranges    JSONB NOT NULL,                    -- faixas dos atributos (crafting híbrido)
    rune_slots     SMALLINT NOT NULL,
    max_durability REAL NOT NULL
);

CREATE TABLE recipes (
    id     TEXT PRIMARY KEY,
    kind   TEXT NOT NULL,                             -- equipment, bait, ...
    inputs JSONB NOT NULL,                            -- {material_id: qty}
    output JSONB NOT NULL
);

CREATE TABLE boss_templates (
    id           TEXT PRIMARY KEY,
    world_id     TEXT NOT NULL REFERENCES worlds(id),
    name         TEXT NOT NULL,
    base_stamina REAL NOT NULL,
    base_force   REAL NOT NULL,
    enrage_every REAL NOT NULL,
    enrage_mult  REAL NOT NULL
);

CREATE TABLE pet_templates (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    base_capacity INT  NOT NULL,                       -- itens por viagem
    base_interval REAL NOT NULL,                       -- segundos por viagem
    traits        JSONB NOT NULL DEFAULT '[]'
);

-- ========== ESTADO DO JOGADOR (UUID) ==========
CREATE TABLE players (
    id                  UUID PRIMARY KEY,
    name                TEXT NOT NULL,
    class               TEXT NOT NULL,
    base_stats          JSONB NOT NULL,
    gold                BIGINT NOT NULL DEFAULT 0,
    level               INT NOT NULL DEFAULT 1,
    xp                  BIGINT NOT NULL DEFAULT 0,
    skill_points        INT NOT NULL DEFAULT 0,
    skill_tree          JSONB NOT NULL DEFAULT '{}',
    filters             JSONB NOT NULL DEFAULT '[]',
    active_pet_id       UUID,                          -- FK lógica p/ player_pets (sem constraint p/ evitar ciclo)
    highest_location_id TEXT REFERENCES locations(id),
    last_logout         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE player_equipment (
    id             UUID PRIMARY KEY,
    player_id      UUID NOT NULL REFERENCES players(id),
    template_id    TEXT NOT NULL REFERENCES equipment_templates(id),
    type           TEXT NOT NULL,                     -- rod, reel, line
    bonus_stats    JSONB NOT NULL,                    -- rolagem server-seeded
    durability     REAL NOT NULL,
    max_durability REAL NOT NULL,
    equipped_slot  TEXT                               -- rod/reel/line ou NULL (no stash)
);
CREATE INDEX ix_equipment_player ON player_equipment(player_id);

CREATE TABLE equipment_runes (
    equipment_id     UUID NOT NULL REFERENCES player_equipment(id) ON DELETE CASCADE,
    slot             SMALLINT NOT NULL,
    rune_template_id TEXT NOT NULL REFERENCES rune_templates(id),
    PRIMARY KEY (equipment_id, slot)
);

CREATE TABLE player_runes (                           -- runas não-engastadas (fungíveis)
    player_id        UUID NOT NULL REFERENCES players(id),
    rune_template_id TEXT NOT NULL REFERENCES rune_templates(id),
    count            INT  NOT NULL CHECK (count >= 0),
    PRIMARY KEY (player_id, rune_template_id)
);

CREATE TABLE player_materials (
    player_id   UUID NOT NULL REFERENCES players(id),
    material_id TEXT NOT NULL REFERENCES material_templates(id),
    count       BIGINT NOT NULL CHECK (count >= 0),
    PRIMARY KEY (player_id, material_id)
);

CREATE TABLE player_baits (
    player_id  UUID NOT NULL REFERENCES players(id),
    bait_id    TEXT NOT NULL,                         -- template da isca
    kind       TEXT NOT NULL,                         -- consumable, durable, boss
    tier       SMALLINT NOT NULL DEFAULT 0,
    charges    INT,                                   -- consumível/boss
    durability REAL,                                  -- durável
    PRIMARY KEY (player_id, bait_id)
);

CREATE TABLE player_trophies (
    id                 UUID PRIMARY KEY,
    player_id          UUID NOT NULL REFERENCES players(id),
    species_id         TEXT NOT NULL REFERENCES fish_templates(id),
    weight             REAL NOT NULL,
    quality            TEXT NOT NULL,                 -- common, rare, epic, legendary, perfect
    caught_location_id TEXT REFERENCES locations(id), -- local de captura
    caught_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ix_trophies_player_species ON player_trophies(player_id, species_id);

CREATE TABLE aquarium (
    player_id     UUID NOT NULL REFERENCES players(id),
    species_id    TEXT NOT NULL REFERENCES fish_templates(id),
    quality       TEXT NOT NULL,                      -- melhor faixa ofertada
    bonus_granted JSONB NOT NULL,
    PRIMARY KEY (player_id, species_id)
);

CREATE TABLE player_boss_clears (                     -- progressão
    player_id UUID NOT NULL REFERENCES players(id),
    boss_id   TEXT NOT NULL REFERENCES boss_templates(id),
    best_tier SMALLINT NOT NULL,
    PRIMARY KEY (player_id, boss_id)
);

CREATE TABLE player_pets (
    id          UUID PRIMARY KEY,
    player_id   UUID NOT NULL REFERENCES players(id),
    template_id TEXT NOT NULL REFERENCES pet_templates(id),
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ix_pets_player ON player_pets(player_id);

-- ========== SESSÃO E BATALHA ==========
CREATE TABLE fishing_session (
    player_id         UUID PRIMARY KEY REFERENCES players(id),
    seed              BIGINT NOT NULL,
    start_time        TIMESTAMPTZ NOT NULL,
    location_id       TEXT NOT NULL REFERENCES locations(id),
    bait_id           TEXT,
    bait_charges_left INT,
    bait_durability   REAL,
    build_snapshot    JSONB NOT NULL,                 -- Stats congelado
    last_index        BIGINT NOT NULL DEFAULT 0,
    last_time         TIMESTAMPTZ NOT NULL,           -- âncora anti-cheat
    backpack_count    INT NOT NULL DEFAULT 0,
    durability        REAL NOT NULL,
    broken            BOOLEAN NOT NULL DEFAULT false,
    auto_repair       BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE boss_battle (
    id             UUID PRIMARY KEY,
    player_id      UUID NOT NULL REFERENCES players(id),
    boss_id        TEXT NOT NULL REFERENCES boss_templates(id),
    tier           SMALLINT NOT NULL,
    seed           BIGINT NOT NULL,
    build_snapshot JSONB NOT NULL,
    start_time     TIMESTAMPTZ NOT NULL,
    status         TEXT NOT NULL DEFAULT 'in_progress' -- in_progress, won, lost
);
CREATE INDEX ix_boss_player_status ON boss_battle(player_id, status);
```

### 7.6. Transação de Claim (ResolveSession)

Todo o lote vira **um bloco atômico** — uma ida ao banco por `claim`:

```sql
BEGIN;

-- 1. Moedas e progressão
UPDATE players
   SET gold  = gold + $delta_gold,
       xp    = xp + $delta_xp,
       level = $new_level,
       skill_points = skill_points + $new_points,
       highest_location_id = COALESCE($new_highest, highest_location_id),
       updated_at = now()
 WHERE id = $player_id;

-- 2. Materiais (upsert por material, em batch)
INSERT INTO player_materials (player_id, material_id, count)
VALUES ($player_id, $mat_id, $qty) -- ...batch...
ON CONFLICT (player_id, material_id)
DO UPDATE SET count = player_materials.count + EXCLUDED.count;

-- 3. Troféus novos (instâncias)
INSERT INTO player_trophies (id, player_id, species_id, weight, quality)
VALUES (gen_random_uuid(), $player_id, $species, $weight, $quality); -- ...batch...

-- 4. Drops raros (equipamento = instância; runa = contagem)
INSERT INTO player_equipment (...) VALUES (...);
INSERT INTO player_runes (player_id, rune_template_id, count)
VALUES ($player_id, $rune, $n)
ON CONFLICT (player_id, rune_template_id)
DO UPDATE SET count = player_runes.count + EXCLUDED.count;

-- 5. Avançar a sessão (âncora anti-cheat)
UPDATE fishing_session
   SET last_index = $last_index,
       last_time  = $now,
       backpack_count = $backpack,
       durability = $durability,
       broken = $broken,
       bait_charges_left = $charges,
       bait_durability = $bait_dur
 WHERE player_id = $player_id;

COMMIT;
```

> A **Oferenda no Aquário** é uma transação à parte: remove o troféu, faz `UPSERT` em `aquarium` mantendo a melhor `quality`, recalcula `bonus_granted` e dispara o recálculo de `CalculateTotalStats`.

---

## 8. Custo e Carga Estimados

| | Custo por jogador |
|---|---|
| **Jogo desligado** | 1 linha no banco · cálculo O(1) no login · 0 conexão |
| **Idle (jogo aberto)** | 1 WS opcional + `Resolve()` esporádico (sub-ms) + cache Redis |
| **Clima** | função determinística (sem custo por jogador) + poucas chaves Redis |

* **Soft-launch (até alguns milhares de DAU):** 1 VPS 2 vCPU/4 GB (~US$ 20–40) + Postgres gerenciado pequeno (~US$ 15–25) + Redis pequeno (US$ 0–15) ≈ **US$ 40–80/mês**.
* **Crescimento (dezenas de milhares de DAU):** o Go é *stateless* nesse desenho → escala horizontal atrás de load balancer. Custo em poucas centenas/mês; o limite aparece primeiro no Postgres.

> O desenho lazy elimina os dois problemas do modelo tick-a-tick original: ~3–5× mais custo de CPU/banda e a dificuldade de escala horizontal com WebSocket por jogador.

---

## 9. Guia de Desenvolvimento Backend

Esta seção é o manual prático de como evoluir o backend sem quebrar os princípios da arquitetura. Leia antes de adicionar conteúdo ou um sistema novo.

### 9.1. Regra de Ouro — o que é Lazy e o que é WebSocket

Esta é a decisão mais importante e a mais fácil de errar. As duas coisas vivem em mundos separados:

* **Lazy / Server-Authoritative** = **tudo que decide ou altera a verdade do jogo.** É calculado no servidor de forma determinística (a partir da seed) e persistido no `claim`. Acontece com ou sem cliente assistindo (inclusive offline).
* **WebSocket** = **só transporte cosmético de saída** de eventos **já resolvidos**, para o cliente animar em foreground. Nunca decide nada, nunca persiste, pode ser perdido sem qualquer impacto no progresso.

Use esta checklist para classificar **qualquer** evento novo:

| Pergunta | Se "sim" → |
|---|---|
| Altera estado persistente (ouro, XP, inventário, progressão, durabilidade)? | **Lazy** (Resolve/UseCase + transação) |
| Precisa ser reproduzível/auditável para anti-cheat? | **Lazy** (derivado da seed) |
| Pode acontecer offline ou sem ninguém assistindo? | **Lazy** (nunca WS) |
| É apenas "mostrar bonito" algo que o servidor já decidiu? | **WebSocket** |
| Vem do cliente afirmando um resultado? | **Nunca confiar** — revalidar no servidor (ex.: replay do boss) |

**Eventos LAZY (no `Resolve()` ou em use-cases transacionais):**
mordida, spawn, captura/escape, duração da luta, desgaste de durabilidade, quebra/auto-reparo, consumo de isca, drops de equip/runa, ganho de ouro/material/troféu, XP e level-up, viagens do Pet e travas de mochila, oferta ao Aquário, craft/re-forja/engaste/alquimia, recompensa de jogo desligado, e a **validação** da Batalha de Boss (`SubmitBossResult` → replay).

**Mensagens WEBSOCKET (saída, foreground):**
* `{"type":"event", ...}` por captura/escape resolvida → animar a vara, o peixe na linha, o número de ouro subindo. Vem de `domain.GameEvent` emitido por `ResolveStream`.
* `{"type":"state", ...}` snapshot pós-lote → atualizar HUD (ouro/XP/durabilidade/clima).
* Aviso de mudança de clima (derivado do clima determinístico) → "tempestade chegando".
* (Futuro) frames de animação da Batalha de Boss enquanto o cliente joga — mas a **autoridade continua no `SubmitBossResult`**.

**Anti-padrões (não faça):**
* ❌ WS recebendo "o cliente pescou X" — o cliente **nunca** decide capturas.
* ❌ timer/goroutine/worker **por jogador** para gerar mordidas — tudo é lazy sob demanda.
* ❌ escrever no Postgres por tick/evento — só no `claim` (ver §7.4/§7.6).
* ❌ confiar em payload do cliente sem revalidar no servidor.

> Referência no código: `internal/domain/resolve.go` (lazy puro), `ResolveStream`/`GameEvent` (emissão p/ WS), e a separação **leitura × escrita** em `internal/usecase/fishing.go` — `PreviewFishing` (GET, recalcula da seed, **zero escrita**) × `ClaimFishing` (POST, persiste no claim). O servidor só escreve no claim.

### 9.2. Camadas (Clean Architecture) — onde cada coisa mora

Dependências apontam **para dentro**: o domínio não importa nada de fora.

| Camada | Pasta (atual/prevista) | Responsabilidade | Regra |
|---|---|---|---|
| **Domínio** | `internal/domain` | regras puras, determinísticas, sem I/O (motor `Resolve`, fórmulas, tipos, `BalanceConfig`) | testável sem banco; nunca importa http/sql/redis |
| **Use Cases** | `internal/usecase` *(a criar)* | orquestram domínio + repositórios + transação (`ResolveSession`, `CraftEquipment`, `OfferToAquarium`…) | nenhuma regra matemática aqui — delega ao domínio |
| **Repositórios** | `internal/repo` *(a criar)* | Postgres/Redis; convertem row ↔ domínio | sem regra de negócio |
| **Delivery** | `cmd/*`, `internal/http`, `internal/ws` *(a criar)* | handlers HTTP/WS, serialização | sem regra de negócio |

Templates (peixes, localizações, runas…) são carregados na RAM no boot e **injetados no `Engine`**; o domínio os trata como read-only.

### 9.3. Como adicionar uma espécie de peixe

É **dado**, não código (sem recompilar). Passos:

1. Inserir em `fish_templates`. Os campos relevantes mudam por `category`:
   * `vendor` → define `gold_value`, `min_weight`/`max_weight` (o motor sorteia o peso; ≥80% do máximo vira troféu, senão auto-venda) e `species_id` (default = id).
   * `material` → define `material_id` (precisa existir em `material_templates`).
   * `rune` → define `rune_template_id` (a captura concede a runa no inventário).
   * `trophy` → sempre troféu individual; define `species_id` e `min_weight`/`max_weight`.
   * `boss` → tratado pelo fluxo da Batalha de Boss.
2. Adicionar a espécie a uma ou mais `spawn_tables` com um `weight` (peso na roleta — maior = mais comum).
3. Definir `rarity` (inteiro) no template — governa o viés da Sorte no spawn e o filtro `min_rarity`.

```sql
-- Ex.: novo peixe comercial/troféu nas Montanhas (1-2)
INSERT INTO fish_templates (id, name, category, rarity, min_weight, max_weight, stamina, force, gold_value, xp, species_id)
VALUES ('marlin_azul', 'Marlim Azul', 'vendor', 3, 20, 180, 95, 70, 120, 90, 'marlin_azul');

INSERT INTO spawn_tables (spawn_table_id, fish_template_id, weight)
VALUES ('spawn_montanhas', 'marlin_azul', 3);  -- raro (peso baixo na roleta)
```

> No domínio Go isso corresponde a `domain.FishTemplate` (`Force` = Força Exigida, `Stamina` = duração da luta, `Rarity` = tier de spawn, `MinWeight`/`MaxWeight` = faixa de tamanho). O boot recarrega os templates; nada de código muda.

### 9.4. Como adicionar uma raridade

Há **dois conceitos distintos** de "raridade":

* **Raridade de spawn** (`fish_templates.rarity`, inteiro): **só dado.** Convenção sugerida: `0` comum, `1` incomum, `2` raro, `3` épico, `4` lendário. Para um novo patamar, basta usar um inteiro maior e calibrar os `weight` da spawn table — a Sorte (proc) já sobe o tier, e os filtros usam `min_rarity`. **Não exige código.**
* **Qualidade de troféu** (`QualityTier`: `common`/`rare`/`epic`/`legendary`/`perfect`): definida pela fração do peso sorteado sobre o tamanho **máximo** da espécie. Adicionar uma faixa (ex.: `mythic`) **é código**: (a) nova constante em `domain` (`types.go`), (b) novo limiar em `BalanceConfig` (`Trophy*Pct`), (c) tratamento em `classifyTrophy` (`trophy.go`), (d) o bônus correspondente no Aquário.

### 9.5. Como adicionar itens (equipamento, runa, isca, pet, material, atributo)

| Item | Onde | Como |
|---|---|---|
| **Equipamento** | `equipment_templates` + `recipes` | template define `type` (rod/reel/line), `roll_ranges` (JSONB com faixas por atributo), `rune_slots`, `max_durability`. Crafting **híbrido**: o servidor rola dentro das faixas (server-seeded). |
| **Runa** | `rune_templates` (+ drop/recipe) | template **fixo** (`bonus_stats` JSONB, `apply_status`). Sem rolagem. Origem: drop (tabela em `BalanceConfig`) ou receita. |
| **Isca** | `recipes` (kind=`bait`) | `output` define `kind` (consumable/durable/boss), `tier`, `bonus_stats`, `charges`/`durability`. Consumível rende lote (ex.: 500 cargas); durável tem durabilidade; boss tem `tier`. |
| **Pet** | `pet_templates` (→ `player_pets`) | `base_capacity`, `base_interval`, `traits`. Colecionável; a Skill Tree aplica multiplicadores **globais**. |
| **Material** | `material_templates` | id + nome; vira `material_id` de um peixe `material` ou subproduto de craft. |
| **Novo atributo (Stat)** | **código** | adicionar campo em `domain.Stats`, tratá-lo em `CalculateTotalStats` e onde for usado no `Resolve`. Como `build_snapshot`/`bonus_stats` são **JSONB**, **não precisa migration**. |

### 9.6. Balanceamento

Os números vivem em **dois lugares**, e saber qual usar é a regra central:

* **Dados no banco (hotfix sem deploy):** forças/estaminas/valores dos peixes, `weight` de spawn, `roll_ranges` de equipamento, `gold_per_hour`/`xp_per_hour` por Localização, params de boss (`base_stamina`, `enrage_*`). Editáveis por seed/admin.
* **Config de fórmula (`internal/domain/config.go`, `BalanceConfig`):** constantes globais de regra — `FightK`, `EscapeBase`, `WearPerCatch`, `BruiserWearMult`, `LuckRescueBase`, `Weather*`, limiares de qualidade (`Trophy*Pct`), dispersão do peso (`WeightSigmaDivisor`, curva de Gauss), chances de drop, `XPPerLevel`, `BrokenPowerMult`, `BasicBaitBiteMult`. Mudar é **código** (deploy).

> Princípio: **número de conteúdo → banco; constante de regra/fórmula → `BalanceConfig`.**

Diretrizes:
* **Curvas:** derive `gold_per_hour`/`xp_per_hour` de uma Localização do **rendimento esperado do Idle** ali; o modo desligado é uma fração disso (`× (1 − X%)`), mantendo os dois acoplados.
* **Bancada de tuning:** com `DEV_MODE=true`, use o client de teste em `/` (fast-forward via `?ff=`) ou exercite o motor por testes/benchmarks em `internal/domain` para **medir** ouro/XP por hora, taxa de captura e frequência de quebra **antes** de cravar números.
* **Auditoria:** como tudo é server-seeded e determinístico, qualquer sessão suspeita é reproduzível a partir de `(seed, build_snapshot, last_index)`.

### 9.7. Estrutura da DB (resumo operacional)

Detalhe completo em §7; o que reter no dia a dia:

* **Três famílias:** *templates* (id em `TEXT`, read-mostly, carregados na RAM no boot), *estado do jogador* (id em `UUID`), *sessão/batalha*.
* **JSONB** para `stats`/`snapshot`/`roll_ranges`/`bonus_stats`/`filters` → o jogo evolui de atributos **sem migration**.
* **Escrita só no `claim`** (transação atômica única, §7.6) e em ações de menu. **Zero escrita por tick.**
* **Migrations:** mudança de **schema** vai por migration versionada (ex.: `golang-migrate`); mudança só de **dado de template** é seed/admin, sem migration.
* **Índices** já previstos: `player_id`, `(player_id, species_id)`, `(player_id, status)`.

### 9.8. Convenções de Código e Testes

* Comentários em **pt-BR** (consistência com o existente). `gofmt` sempre; `go vet ./...` e `go test ./...` verdes antes de commit.
* O **domínio é 100% testável sem I/O** — toda mecânica nova entra com teste.
* **Propriedades invioláveis** (cobertas por testes em `resolve_test.go`, mantenha-as):
  1. **Determinismo:** mesma seed → resultado idêntico.
  2. **Aditividade:** `Resolve(s, T)` ≡ `Resolve(s, T1)` + `Resolve(s, T)` (online ≡ offline-aberto).
  3. **Commit atômico de evento:** estado só avança em eventos completos; um evento que não cabe na janela é deferido sem deixar rastro.