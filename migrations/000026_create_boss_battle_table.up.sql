-- Create boss_battle table (registro pendente/concluído de batalha)
CREATE TABLE IF NOT EXISTS boss_battle (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id      UUID NOT NULL,
    boss_id        TEXT NOT NULL,
    tier           SMALLINT NOT NULL,
    seed           BIGINT NOT NULL,
    build_snapshot JSONB NOT NULL,
    start_time     TIMESTAMPTZ NOT NULL,
    status         TEXT NOT NULL DEFAULT 'in_progress', -- in_progress, won, lost
    CONSTRAINT boss_battle_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT boss_battle_boss_id_fk FOREIGN KEY (boss_id) REFERENCES boss_templates(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS ix_boss_player_status ON boss_battle(player_id, status);
