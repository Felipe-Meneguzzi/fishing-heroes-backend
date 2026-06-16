-- Create aquarium table (melhor troféu por espécie + buff global)
CREATE TABLE IF NOT EXISTS aquarium (
    player_id     UUID NOT NULL,
    species_id    TEXT NOT NULL,
    quality       TEXT NOT NULL,
    bonus_granted JSONB NOT NULL,
    PRIMARY KEY (player_id, species_id),
    CONSTRAINT aquarium_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT aquarium_species_id_fk FOREIGN KEY (species_id) REFERENCES fish_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
