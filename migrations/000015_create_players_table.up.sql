-- Create players table
CREATE TABLE IF NOT EXISTS players (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL,
    class               TEXT NOT NULL,
    base_stats          JSONB NOT NULL,
    gold                BIGINT NOT NULL DEFAULT 0,
    level               INT NOT NULL DEFAULT 1,
    xp                  BIGINT NOT NULL DEFAULT 0,
    skill_points        INT NOT NULL DEFAULT 0,
    skill_tree          JSONB NOT NULL DEFAULT '{}',
    filters             JSONB NOT NULL DEFAULT '[]',
    active_bait_id      TEXT,
    active_pet_id       UUID,                   -- FK lógica p/ player_pets
    highest_location_id TEXT,
    last_logout         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT players_class_fk FOREIGN KEY (class) REFERENCES class_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT players_highest_location_id_fk FOREIGN KEY (highest_location_id) REFERENCES locations(id) ON DELETE SET NULL ON UPDATE CASCADE
);
