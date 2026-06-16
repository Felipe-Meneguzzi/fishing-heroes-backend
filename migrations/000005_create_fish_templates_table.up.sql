-- Create fish_templates table
CREATE TABLE IF NOT EXISTS fish_templates (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    category         TEXT NOT NULL,            -- vendor, material, rune, trophy, boss
    rarity           INT  NOT NULL DEFAULT 0,  -- viés da Sorte no spawn / min_rarity dos filtros
    min_weight       REAL NOT NULL DEFAULT 0,  -- tamanho mínimo sorteável (kg)
    max_weight       REAL NOT NULL DEFAULT 0,  -- tamanho máximo; base das faixas de troféu (% do máx)
    stamina          REAL NOT NULL,            -- duração da luta
    force            REAL NOT NULL,            -- Força Exigida
    gold_value       BIGINT NOT NULL DEFAULT 0,
    xp               BIGINT NOT NULL DEFAULT 0,
    material_id      TEXT,                     -- categoria material
    rune_template_id TEXT,                     -- categoria rune
    species_id       TEXT                      -- espécie p/ troféu/Aquário (default = id)
);
