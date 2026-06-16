-- Create fishing_session table (uma linha por jogador; reconstrói o offline)
CREATE TABLE IF NOT EXISTS fishing_session (
    player_id         UUID PRIMARY KEY,
    seed              BIGINT NOT NULL,
    start_time        TIMESTAMPTZ NOT NULL,
    location_id       TEXT NOT NULL,
    bait_id           TEXT,
    bait_charges_left INT,
    bait_durability   REAL,
    build_snapshot    JSONB NOT NULL,           -- Stats + classe + max_durability
    last_index        BIGINT NOT NULL DEFAULT 0,
    last_time         TIMESTAMPTZ NOT NULL,      -- âncora anti-cheat
    backpack_count    INT NOT NULL DEFAULT 0,
    durability        REAL NOT NULL,
    broken            BOOLEAN NOT NULL DEFAULT false,
    auto_repair       BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT fishing_session_player_id_fk FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fishing_session_location_id_fk FOREIGN KEY (location_id) REFERENCES locations(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
