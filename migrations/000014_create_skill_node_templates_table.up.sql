-- Create skill_node_templates table (Skill Tree em cruz)
CREATE TABLE IF NOT EXISTS skill_node_templates (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    branch         TEXT NOT NULL,              -- core, power, luck, speed, tension
    requires       TEXT,                       -- pré-requisito (NULL = raiz)
    max_rank       SMALLINT NOT NULL DEFAULT 1,
    bonus_per_rank JSONB NOT NULL,
    generic        BOOLEAN NOT NULL DEFAULT false
);
