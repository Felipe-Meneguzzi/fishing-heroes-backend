-- Create rune_templates table
CREATE TABLE IF NOT EXISTS rune_templates (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    bonus_stats  JSONB NOT NULL,
    apply_status TEXT                          -- bleed, etc. (NULL = nenhum)
);
