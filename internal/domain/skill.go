package domain

// Skill Tree em cruz (＋): um nó central GENÉRICO (raiz) e quatro braços, cada um
// especializado num foco de atributo. O jogador investe pontos (ganhos ao subir
// de nível) e os ranks ficam no Player.SkillTree (nodeID -> rank). As definições
// dos nós são templates read-only (tabela skill_node_templates, carregados na RAM).
//
//	            [força]
//	               │
//	  [tensão] ──[centro]── [sorte]
//	               │
//	          [velocidade]
//
// Cada braço requer o nó central; o central é a raiz (sem pré-requisito).

// SkillBranch — ramo/foco de um nó da árvore.
type SkillBranch string

const (
	BranchCore    SkillBranch = "core"    // central genérico
	BranchPower   SkillBranch = "power"   // força (foco do Brutamontes)
	BranchLuck    SkillBranch = "luck"    // sorte/raridade
	BranchSpeed   SkillBranch = "speed"   // velocidade de atração (foco do Estrategista)
	BranchTension SkillBranch = "tension" // resistência da linha / escape
)

// SkillNode — definição (template) de um nó da Skill Tree.
type SkillNode struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Branch       SkillBranch `json:"branch"`
	Requires     string      `json:"requires"`     // id do pré-requisito ("" = raiz)
	MaxRank      int         `json:"maxRank"`      // tetos de investimento
	BonusPerRank Stats       `json:"bonusPerRank"` // bônus somado por rank investido
	Generic      bool        `json:"generic"`      // true só no nó central
}
