-- Create recipes table
CREATE TABLE IF NOT EXISTS recipes (
    id     TEXT PRIMARY KEY,
    kind   TEXT NOT NULL,                      -- equipment, bait, ...
    inputs JSONB NOT NULL,                     -- {material_id: qty}
    output JSONB NOT NULL
);
