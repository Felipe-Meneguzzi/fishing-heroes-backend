-- Create player_materials table
CREATE TABLE IF NOT EXISTS player_materials (
    player_id   UUID NOT NULL,
    material_id TEXT NOT NULL,
    count       BIGINT NOT NULL CHECK (count >= 0),
    PRIMARY KEY (player_id, material_id),
    CONSTRAINT player_materials_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_materials_material_id_fk FOREIGN KEY (material_id) REFERENCES material_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
