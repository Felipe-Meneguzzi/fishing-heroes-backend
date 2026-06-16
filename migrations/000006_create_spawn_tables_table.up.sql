-- Create spawn_tables table
CREATE TABLE IF NOT EXISTS spawn_tables (
    spawn_table_id   TEXT NOT NULL,
    fish_template_id TEXT NOT NULL,
    weight           INT  NOT NULL,           -- peso na roleta de spawn
    PRIMARY KEY (spawn_table_id, fish_template_id),
    CONSTRAINT spawn_tables_fish_template_id_fk FOREIGN KEY (fish_template_id) REFERENCES fish_templates(id) ON DELETE CASCADE ON UPDATE CASCADE
);
