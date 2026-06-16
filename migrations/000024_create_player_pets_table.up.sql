-- Create player_pets table (pets colecionáveis; um ativo por vez)
CREATE TABLE IF NOT EXISTS player_pets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id   UUID NOT NULL,
    template_id TEXT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT player_pets_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT player_pets_template_id_fk FOREIGN KEY (template_id) REFERENCES pet_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS ix_pets_player ON player_pets(player_id);
