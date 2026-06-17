ALTER TABLE players DROP CONSTRAINT IF EXISTS players_account_id_unique;
ALTER TABLE players DROP CONSTRAINT IF EXISTS players_account_id_fk;
ALTER TABLE players DROP COLUMN IF EXISTS account_id;
