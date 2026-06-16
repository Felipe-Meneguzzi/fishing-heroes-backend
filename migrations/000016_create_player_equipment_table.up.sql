-- Create player_equipment table
CREATE TABLE IF NOT EXISTS player_equipment (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id      UUID NOT NULL,
    template_id    TEXT NOT NULL,
    type           TEXT NOT NULL,              -- rod, reel, line
    bonus_stats    JSONB NOT NULL,             -- rolagem server-seeded
    durability     REAL NOT NULL,
    max_durability REAL NOT NULL,
    equipped_slot  TEXT,                       -- rod/reel/line ou NULL (no stash)
    CONSTRAINT player_equipment_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_equipment_template_id_fk FOREIGN KEY (template_id) REFERENCES equipment_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS ix_equipment_player ON player_equipment(player_id);
