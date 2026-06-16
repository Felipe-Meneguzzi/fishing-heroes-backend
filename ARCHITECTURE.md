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

### C. Batalha de Boss
Sistema à parte (disparado pela isca alquímica do mundo), ainda em definição. Provavelmente o único fluxo com simulação mais ativa — será desenhado para **não** comprometer o modelo lazy do fluxo comum.

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
	Category   string  `json:"category"` // vendor (auto-venda), material, alchemy_bait, boss, trophy
	Weight     float64 `json:"weight"`   // valor BASE; troféus sorteiam peso/qualidade por instância
	Stamina    float64 `json:"stamina"`  // determina a DURAÇÃO da luta (÷ poder do jogador)
	Force      float64 `json:"force"`    // Força Exigida: captura se poder >= Force (senão a Sorte pode resgatar)
}

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
	Materials map[string]int    `json:"materials"`
	Trophies  []*TrophyInstance `json:"trophies"`
	Pet       *Pet              `json:"pet"`
	SkillTree SkillTree         `json:"skill_tree"`
	Aquarium  Aquarium          `json:"aquarium"` // progressão global passiva
}

// CalculateTotalStats aplica Base + Classe + Equipamentos + Runas + SkillTree + Aquário
func (p *Player) CalculateTotalStats() Stats {
	total := p.BaseStats

	// 1. Aplicar multiplicadores da Classe
	if p.Class == ClassBruiser {
		total.FishingPower *= 1.20 // +20% força
	} else if p.Class == ClassTrapper {
		total.DoubleCatchChance += 0.05
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

const (
	QualityCommon  QualityTier = "common"
	QualityGreat   QualityTier = "great"
	QualityPerfect QualityTier = "perfect"
)

// Troféu: única categoria de peixe guardada como instância individual.
type TrophyInstance struct {
	InstanceID string      `json:"instance_id"`
	SpeciesID  string      `json:"species_id"`
	Weight     float64     `json:"weight"`  // sorteado na captura
	Quality    QualityTier `json:"quality"` // faixa que escala o bônus no Aquário
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

---

## 7. Modelo de Dados e Persistência

### 7.1. Fonte da Verdade (PostgreSQL)
* `players` — conta, classe, `base_stats`, ouro, skill tree.
* `player_equipment` — instâncias de vara/molinete/linha, durabilidade, slots.
* `equipment_runes` — runas engastadas por equipamento.
* `player_materials` — contagens fungíveis por material.
* `player_trophies` — troféus individuais (peso, qualidade).
* `aquarium` — melhor troféu por espécie + bônus cacheado.
* `fishing_session` — uma linha por jogador (ver `FishingSession`).

### 7.2. Estado Quente / Efêmero (Redis)
* `Loc:{id}:Weather` — publicação do clima atual por Localização, com TTL (apenas cache pra UI; o clima é determinístico).
* Cache opcional de sessões em foreground.
* Pode ser perdido sem perda de progresso (Postgres é a verdade).

### 7.3. Templates (RAM)
Peixes, Mundos/Localizações, tabelas de spawn, runas e receitas são read-only e carregados em memória no boot.

### 7.4. Cadência de Escrita
* **Zero escrita por tick.**
* `claim`/fim de lote: uma transação aplica os deltas (ouro +=, materiais +=, insere troféus, atualiza Aquário) e atualiza `last_index`/`last_time`.
* Ações de menu (craft, alquimia, engaste, oferenda): transacionais e imediatas.
* **Gargalo real do sistema = TPS/conexões do Postgres** (não a CPU do Go). Por isso a escrita é enxuta e em lote.

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