-- Create locations table
CREATE TABLE IF NOT EXISTS locations (
    id             TEXT PRIMARY KEY,   -- ex.: "1-1" (mundo 1, localização 1)
    world_id       TEXT NOT NULL,
    name           TEXT NOT NULL,      -- ex.: "Campos"
    level          INT  NOT NULL,
    spawn_table_id TEXT NOT NULL,
    weather_seed   BIGINT NOT NULL,    -- base do clima determinístico
    base_bite_time REAL   NOT NULL,    -- X base (segundos) antes da build
    gold_per_hour  BIGINT NOT NULL,    -- baseline do modo desligado
    xp_per_hour    BIGINT NOT NULL,
    CONSTRAINT locations_world_id_fk FOREIGN KEY (world_id) REFERENCES worlds(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
