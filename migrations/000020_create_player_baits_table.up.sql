-- Create player_baits table
CREATE TABLE IF NOT EXISTS player_baits (
    player_id  UUID NOT NULL,
    bait_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,                  -- consumable, durable, boss
    tier       SMALLINT NOT NULL DEFAULT 0,
    charges    INT,                            -- consumível/boss
    durability REAL,                           -- durável
    PRIMARY KEY (player_id, bait_id),
    CONSTRAINT player_baits_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_baits_bait_id_fk FOREIGN KEY (bait_id) REFERENCES bait_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
