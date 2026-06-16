-- Create pet_templates table
CREATE TABLE IF NOT EXISTS pet_templates (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    base_capacity INT  NOT NULL,               -- itens por viagem
    base_interval REAL NOT NULL,               -- segundos por viagem
    traits        JSONB NOT NULL DEFAULT '[]'
);
