-- Create market_listings table (anúncios de troféus/equipamentos).
-- item_id é polimórfico (trophy|equipment), por isso sem FK direta.
-- Campos steam_* reservados para a futura ponte com o Steam Community Market.
CREATE TABLE IF NOT EXISTS market_listings (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_player_id UUID NOT NULL,
    item_type        TEXT NOT NULL,              -- trophy, equipment
    item_id          UUID NOT NULL,
    price            BIGINT NOT NULL CHECK (price > 0),
    fee              BIGINT NOT NULL DEFAULT 0,  -- taxa do mercado (sink)
    status           TEXT NOT NULL DEFAULT 'active', -- active, sold, cancelled
    buyer_player_id  UUID,
    steam_backed     BOOLEAN NOT NULL DEFAULT false, -- reservado p/ Steam Market
    steam_listing_id TEXT,                       -- reservado
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT market_listings_seller_fk FOREIGN KEY (seller_player_id) REFERENCES players(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT market_listings_buyer_fk  FOREIGN KEY (buyer_player_id)  REFERENCES players(id) ON DELETE SET NULL ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS ix_market_active ON market_listings(status, item_type, created_at);
CREATE INDEX IF NOT EXISTS ix_market_seller ON market_listings(seller_player_id, status);
-- Um item só pode ter um anúncio ativo.
CREATE UNIQUE INDEX IF NOT EXISTS ux_market_active_item ON market_listings(item_id) WHERE status = 'active';
