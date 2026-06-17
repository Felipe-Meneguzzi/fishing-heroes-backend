package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrListingNotFound  = errors.New("anúncio não encontrado")
	ErrItemUnavailable  = errors.New("item indisponível (inexistente, equipado ou já anunciado)")
	ErrInsufficientGold = errors.New("ouro insuficiente")
	ErrSelfPurchase     = errors.New("não é possível comprar o próprio anúncio")
)

// ListingView — anúncio do mercado com detalhes do item (para exibição).
type ListingView struct {
	ID         string    `json:"id"`
	SellerID   string    `json:"sellerId"`
	ItemType   string    `json:"itemType"`
	ItemID     string    `json:"itemId"`
	Price      int64     `json:"price"`
	Fee        int64     `json:"fee"`
	Status     string    `json:"status"`
	BuyerID    string    `json:"buyerId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	SpeciesID  string    `json:"speciesId,omitempty"`  // troféu
	Weight     float64   `json:"weight,omitempty"`     // troféu
	Quality    string    `json:"quality,omitempty"`    // troféu
	TemplateID string    `json:"templateId,omitempty"` // equipamento
	EquipType  string    `json:"equipType,omitempty"`  // equipamento
}

// CreateListing coloca um troféu/equipamento à venda (escrow: marca on_market).
func CreateListing(ctx context.Context, pool *pgxpool.Pool, sellerID, itemType, itemID string, price, fee int64) (string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var tag pgconn.CommandTag
	switch itemType {
	case "trophy":
		tag, err = tx.Exec(ctx, `UPDATE player_trophies SET on_market = true
			WHERE id = $1 AND player_id = $2 AND on_market = false`, itemID, sellerID)
	case "equipment":
		tag, err = tx.Exec(ctx, `UPDATE player_equipment SET on_market = true
			WHERE id = $1 AND player_id = $2 AND on_market = false AND equipped_slot IS NULL`, itemID, sellerID)
	default:
		return "", fmt.Errorf("tipo de item inválido: %s", itemType)
	}
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrItemUnavailable
	}

	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO market_listings
		(seller_player_id, item_type, item_id, price, fee) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		sellerID, itemType, itemID, price, fee).Scan(&id); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

// CancelListing devolve o item ao vendedor e fecha o anúncio.
func CancelListing(ctx context.Context, pool *pgxpool.Pool, sellerID, listingID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var itemType, itemID string
	err = tx.QueryRow(ctx, `SELECT item_type, item_id FROM market_listings
		WHERE id = $1 AND seller_player_id = $2 AND status = 'active' FOR UPDATE`,
		listingID, sellerID).Scan(&itemType, &itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrListingNotFound
	}
	if err != nil {
		return err
	}
	if err := clearOnMarket(ctx, tx, itemType, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE market_listings SET status = 'cancelled', updated_at = now() WHERE id = $1`, listingID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BuyListing transfere o item ao comprador e o ouro ao vendedor (menos a taxa).
func BuyListing(ctx context.Context, pool *pgxpool.Pool, buyerID, listingID string) (*ListingView, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var v ListingView
	v.ID = listingID
	err = tx.QueryRow(ctx, `SELECT seller_player_id, item_type, item_id, price, fee
		FROM market_listings WHERE id = $1 AND status = 'active' FOR UPDATE`, listingID).
		Scan(&v.SellerID, &v.ItemType, &v.ItemID, &v.Price, &v.Fee)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrListingNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.SellerID == buyerID {
		return nil, ErrSelfPurchase
	}

	// Debita o comprador (só se tiver ouro suficiente).
	tag, err := tx.Exec(ctx, `UPDATE players SET gold = gold - $2, updated_at = now()
		WHERE id = $1 AND gold >= $2`, buyerID, v.Price)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrInsufficientGold
	}
	// Credita o vendedor (preço menos a taxa do mercado — a taxa é sink).
	if _, err := tx.Exec(ctx, `UPDATE players SET gold = gold + $2, updated_at = now() WHERE id = $1`,
		v.SellerID, v.Price-v.Fee); err != nil {
		return nil, err
	}
	// Transfere o item ao comprador, tirando do escrow.
	if err := transferItem(ctx, tx, v.ItemType, v.ItemID, buyerID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE market_listings SET status = 'sold', buyer_player_id = $2, updated_at = now() WHERE id = $1`,
		listingID, buyerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	v.Status = "sold"
	v.BuyerID = buyerID
	return &v, nil
}

// BrowseListings lista anúncios ativos (filtra por tipo se itemType != "").
func BrowseListings(ctx context.Context, pool *pgxpool.Pool, itemType string, limit int) ([]ListingView, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return queryListings(ctx, pool, `WHERE l.status = 'active' AND ($1 = '' OR l.item_type = $1)
		ORDER BY l.created_at DESC LIMIT $2`, itemType, limit)
}

// MyListings lista os anúncios ativos de um vendedor.
func MyListings(ctx context.Context, pool *pgxpool.Pool, sellerID string) ([]ListingView, error) {
	return queryListings(ctx, pool, `WHERE l.status = 'active' AND l.seller_player_id = $1
		ORDER BY l.created_at DESC LIMIT $2`, sellerID, 200)
}

func queryListings(ctx context.Context, pool *pgxpool.Pool, where string, arg1 any, arg2 int) ([]ListingView, error) {
	rows, err := pool.Query(ctx, `SELECT l.id, l.seller_player_id, l.item_type, l.item_id, l.price, l.fee, l.status, l.created_at,
		t.species_id, t.weight, t.quality, e.template_id, e.type
		FROM market_listings l
		LEFT JOIN player_trophies  t ON l.item_type = 'trophy'    AND t.id = l.item_id
		LEFT JOIN player_equipment e ON l.item_type = 'equipment' AND e.id = l.item_id
		`+where, arg1, arg2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ListingView
	for rows.Next() {
		var v ListingView
		var sp, q, tmpl, et *string
		var w *float64
		if err := rows.Scan(&v.ID, &v.SellerID, &v.ItemType, &v.ItemID, &v.Price, &v.Fee, &v.Status, &v.CreatedAt,
			&sp, &w, &q, &tmpl, &et); err != nil {
			return nil, err
		}
		if sp != nil {
			v.SpeciesID = *sp
		}
		if w != nil {
			v.Weight = *w
		}
		if q != nil {
			v.Quality = *q
		}
		if tmpl != nil {
			v.TemplateID = *tmpl
		}
		if et != nil {
			v.EquipType = *et
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- helpers ---

func clearOnMarket(ctx context.Context, tx pgx.Tx, itemType, itemID string) error {
	switch itemType {
	case "trophy":
		_, err := tx.Exec(ctx, `UPDATE player_trophies SET on_market = false WHERE id = $1`, itemID)
		return err
	case "equipment":
		_, err := tx.Exec(ctx, `UPDATE player_equipment SET on_market = false WHERE id = $1`, itemID)
		return err
	}
	return fmt.Errorf("tipo de item inválido: %s", itemType)
}

func transferItem(ctx context.Context, tx pgx.Tx, itemType, itemID, buyerID string) error {
	switch itemType {
	case "trophy":
		_, err := tx.Exec(ctx, `UPDATE player_trophies SET player_id = $2, on_market = false WHERE id = $1`, itemID, buyerID)
		return err
	case "equipment":
		_, err := tx.Exec(ctx, `UPDATE player_equipment SET player_id = $2, on_market = false, equipped_slot = NULL WHERE id = $1`, itemID, buyerID)
		return err
	}
	return fmt.Errorf("tipo de item inválido: %s", itemType)
}
