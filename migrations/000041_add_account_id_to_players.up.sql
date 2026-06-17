-- Vincula o jogador a uma conta (1 jogador por conta nesta fase)
ALTER TABLE players ADD COLUMN IF NOT EXISTS account_id UUID;
ALTER TABLE players ADD CONSTRAINT players_account_id_fk
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL ON UPDATE CASCADE;
ALTER TABLE players ADD CONSTRAINT players_account_id_unique UNIQUE (account_id);
