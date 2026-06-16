-- Create equipment_runes table (runas engastadas)
CREATE TABLE IF NOT EXISTS equipment_runes (
    equipment_id     UUID NOT NULL,
    slot             SMALLINT NOT NULL,
    rune_template_id TEXT NOT NULL,
    PRIMARY KEY (equipment_id, slot),
    CONSTRAINT equipment_runes_equipment_id_fk FOREIGN KEY (equipment_id) REFERENCES player_equipment(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT equipment_runes_rune_template_id_fk FOREIGN KEY (rune_template_id) REFERENCES rune_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
