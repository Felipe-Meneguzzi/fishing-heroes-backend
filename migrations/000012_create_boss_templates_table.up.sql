-- Create boss_templates table
CREATE TABLE IF NOT EXISTS boss_templates (
    id           TEXT PRIMARY KEY,
    world_id     TEXT NOT NULL,
    name         TEXT NOT NULL,
    base_stamina REAL NOT NULL,
    base_force   REAL NOT NULL,
    enrage_every REAL NOT NULL,
    enrage_mult  REAL NOT NULL,
    CONSTRAINT boss_templates_world_id_fk FOREIGN KEY (world_id) REFERENCES worlds(id) ON DELETE RESTRICT ON UPDATE CASCADE
);
