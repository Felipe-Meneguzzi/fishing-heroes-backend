-- Create worlds table (Mundo; hierarquia Mundo › Localização)
CREATE TABLE IF NOT EXISTS worlds (
    id          TEXT PRIMARY KEY,      -- identificador legível, ex.: "1"
    name        TEXT NOT NULL,         -- ex.: "Floresta"
    ordering    INT  NOT NULL,         -- progressão linear
    act_boss_id TEXT NOT NULL          -- boss de estágio (FK lógica p/ boss_templates)
);
