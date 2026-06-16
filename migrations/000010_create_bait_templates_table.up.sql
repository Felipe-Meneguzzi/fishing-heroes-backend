-- Create bait_templates table
CREATE TABLE IF NOT EXISTS bait_templates (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,                -- consumable, durable, boss
    tier        SMALLINT NOT NULL DEFAULT 0,
    bonus_stats JSONB NOT NULL DEFAULT '{}',
    charges     INT,                          -- consumível/boss (lote)
    durability  REAL                          -- durável
);
