-- Create player_boss_clears table (progressão)
CREATE TABLE IF NOT EXISTS player_boss_clears (
    player_id UUID NOT NULL,
    boss_id   TEXT NOT NULL,
    best_tier SMALLINT NOT NULL,
    PRIMARY KEY (player_id, boss_id),
    CONSTRAINT player_boss_clears_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_boss_clears_boss_id_fk FOREIGN KEY (boss_id) REFERENCES boss_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
