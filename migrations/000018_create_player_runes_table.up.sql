-- Create player_runes table (runas não-engastadas, fungíveis)
CREATE TABLE IF NOT EXISTS player_runes (
    player_id        UUID NOT NULL,
    rune_template_id TEXT NOT NULL,
    count            INT  NOT NULL CHECK (count >= 0),
    PRIMARY KEY (player_id, rune_template_id),
    CONSTRAINT player_runes_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_runes_rune_template_id_fk FOREIGN KEY (rune_template_id) REFERENCES rune_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
