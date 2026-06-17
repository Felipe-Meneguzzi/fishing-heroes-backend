-- Marca itens em escrow no mercado (não podem ser usados/equipados enquanto anunciados)
ALTER TABLE player_trophies   ADD COLUMN IF NOT EXISTS on_market BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE player_equipment  ADD COLUMN IF NOT EXISTS on_market BOOLEAN NOT NULL DEFAULT false;
