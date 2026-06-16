-- Create equipment_templates table
CREATE TABLE IF NOT EXISTS equipment_templates (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    type           TEXT NOT NULL,             -- rod, reel, line
    roll_ranges    JSONB NOT NULL,            -- faixas dos atributos (crafting híbrido)
    rune_slots     SMALLINT NOT NULL DEFAULT 0,
    max_durability REAL NOT NULL
);
