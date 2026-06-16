# Arquitetura de Sistema: Idle Fishing RPG

Este documento descreve a arquitetura técnica para um jogo Idle RPG de Pesca multiplayer, focado em alta escalabilidade, segurança contra fraudes (*Server Authoritative*), simulação de progresso offline, e sistemas de progressão profunda (Runas, Classes, Alquimia e Aquário).

---

## 1. Visão Geral da Stack

* **Backend (Servidor)**: Golang (Go) - Alta performance, concorrência com Goroutines e baixo uso de recursos por sessão.
* **Cliente (Engine)**: Godot 4.x - Integração nativa com WebSockets e renderização assíncrona baseada em eventos (Signals).
* **Banco de Dados Principal**: PostgreSQL - Armazenamento persistente de contas, inventários, equipamentos, Runas e dados do Aquário.
* **Cache em Memória**: Redis - Estado ativo da pescaria (timer de mordida), clima global (Weather) da zona e eventos assíncronos do Pet.

---

## 2. Filosofia Central: Desacoplamento e Autoridade do Servidor

* **Server-Authoritative**: O cliente Godot é apenas um visualizador. Toda decisão matemática (clima, status effects das Runas, sucesso do cabo de guerra, itens gerados) ocorre estritamente no backend.
* **Simulação Instantânea**: A luta é simulada de forma determinística na CPU em microssegundos (processamento de ticks e *status effects* por loop de luta).
* **Persistência Assíncrona**: O loop de pescaria escreve dados voláteis no Redis. O banco relacional (Postgres) é atualizado apenas em eventos cruciais.

---

## 3. Arquitetura do Servidor (Golang)

### 3.1. Camada de Domínio (Core Domain)
Contém as regras matemáticas puras, o motor de cabo de guerra com processamento de `StatusEffects` (sangramento, fúria) e as fórmulas que consideram a `ClassType` e os modificadores globais do `Aquarium`.

### 3.2. Casos de Uso (Use Cases)
Orquestram os fluxos de regras de negócio:
* `StartFishingSession()`: Coloca o jogador no estado de pesca, avalia o Clima da Zona e calcula a primeira mordida.
* `ResolveBiteEvent()`: Roda a simulação da luta aplicando *status effects*, calcula desgaste da durabilidade e gera as recompensas.
* `OfferToAquarium()`: Retira um peixe raro da mochila e o registra no Aquário, recalculando os buffs globais do jogador.
* `CraftAlchemyBait()`: Consome materiais da mochila para gerar Iscas Especiais (Alquimia).

### 3.3. Delivery Layer (Comunicação)
* **WebSockets**: Eventos de pescaria em tempo real para animação do Godot.
* **HTTP REST API**: Autenticação, configurações do menu, trocas de Classe, Engastes de Runas, Alquimia e gestão do Aquário.

### 3.4. Motor de Pesca e Eventos Globais (Cron Workers)
1. **Bite Timer**: O servidor calcula a próxima mordida (`NextBite`) com base na Isca, Classe (Estrategistas atraem mais rápido) e Clima.
2. **Weather Worker**: Um cron job global que atualiza periodicamente o `WeatherType` das zonas no Redis (ex: gerando uma Tempestade de 1 hora que afeta todos os jogadores daquela zona).
3. **Pet Courier Worker**: Checa periodicamente os tempos de viagem para transferência de itens ao Stash.

---

## 4. O Padrão Controller-UseCase-Repository

### Controllers
* **WebSocket Router**: Recebe comandos instantâneos de mudança de zona.
* **REST HTTP Handlers**: Recebe builds (troca de runas) e requisições do Porto (Crafting, Alquimia).
* **Global/Local Workers**: Timers assíncronos de mordida e mudança de Clima global.

### Repositories
* **Redis Repository**: Leitura e escrita do status atual do jogador e do clima da zona (`Zone:1:Weather`).
* **PostgreSQL Repository**: Gravação definitiva do jogador, Aquário, Runas engastadas e inventários.

---

## 5. Fluxos de Exemplo

### A. Fluxo de Luta Dinâmica (Tick Loop na RAM)
1. **Server Timer**: O timer da próxima mordida expira no Redis.
2. **Pre-Luta**: O servidor avalia a *Isca Alquímica* equipada, a Classe do jogador e o *Clima*. Com isso, sorteia o Peixe.
3. **Tick a Tick (Go loop)**: 
   * Lê as *Runas* do jogador. Ex: A *Runa Farpada* aplica o status "Sangramento" no peixe (ele perde X de stamina passivamente a cada tick).
   * O peixe pode acionar efeitos próprios, como "Fúria" (aumentando sua Força e dano à durabilidade).
4. **Resolução**: Se capturado, o peixe vai pra mochila (ou processa auto-venda). Envia log via WebSocket para o Godot reproduzir.

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
	BaitSpeed         float64
	DoubleCatchChance float64 // Atributo novo para a classe Trapper ou Runas específicas
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
	BonusStats    Stats     `json:"bonus_stats"`
	MaxDurability float64   `json:"max_durability"`
	Durability    float64   `json:"durability"`
	Runes         []*Rune   `json:"runes"` // Slots de engaste do equipamento
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
	Category   string  `json:"category"` // vendor, material, alchemy_bait, boss
	Weight     float64 `json:"weight"`
	Stamina    float64 `json:"stamina"`
	Force      float64 `json:"force"`
}

type WeatherType string

const (
	WeatherClear WeatherType = "clear"
	WeatherStorm WeatherType = "storm" // Aumenta força de fuga dos peixes, mas viabiliza Bosses
)
```

### 6.4. Aquário Monumental (Progressão Global)
```go
type AquariumDisplay struct {
	FishTemplateID string `json:"fish_template_id"`
	BonusGranted   Stats  `json:"bonus_granted"` // Buff global passivo provido por este peixe
}

type Aquarium struct {
	DisplayedFish map[string]AquariumDisplay `json:"displayed_fish"`
}
```

### 6.5. Entidade do Jogador Consolidada
```go
type Player struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Class        ClassType `json:"class"`
	BaseStats    Stats     `json:"base_stats"`
	
	// Build Atual
	EquippedRod  *Equipment `json:"equipped_rod"`
	EquippedReel *Equipment `json:"equipped_reel"`
	EquippedLine *Equipment `json:"equipped_line"`
	ActiveBaitID string     `json:"active_bait_id"` // Fundamental para invocar Bosses (Alquimia)
	
	// Sistemas de Backing e Progressão
	Backpack     []*Fish    `json:"backpack"`
	Stash        []*Fish    `json:"stash"`
	Pet          *Pet       `json:"pet"`
	SkillTree    SkillTree  `json:"skill_tree"`
	Aquarium     Aquarium   `json:"aquarium"` // O "Cubo" de progressão passiva
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