-- Create player_trophies table (instâncias individuais)
CREATE TABLE IF NOT EXISTS player_trophies (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id          UUID NOT NULL,
    species_id         TEXT NOT NULL,
    weight             REAL NOT NULL,
    quality            TEXT NOT NULL,          -- common, rare, epic, legendary, perfect
    caught_location_id TEXT,                   -- local de captura
    caught_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT player_trophies_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_trophies_species_id_fk FOREIGN KEY (species_id) REFERENCES fish_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT player_trophies_caught_location_id_fk FOREIGN KEY (caught_location_id) REFERENCES locations(id) ON DELETE SET NULL ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS ix_trophies_player_species ON player_trophies(player_id, species_id);
