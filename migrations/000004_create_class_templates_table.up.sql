-- Create class_templates table (base_stats cobre todos os atributos de domain.Stats)
CREATE TABLE IF NOT EXISTS class_templates (
    id          TEXT PRIMARY KEY,      -- bruiser, trapper, ...
    name        TEXT NOT NULL,         -- "Brutamontes", "Estrategista"
    description TEXT NOT NULL DEFAULT '',
    base_stats  JSONB NOT NULL
);
