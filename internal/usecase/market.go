package usecase

import (
	"context"
	"fmt"

	"fishingheroes/internal/repo"
)

// ListItem anuncia um troféu/equipamento no mercado por `price` de ouro.
// A taxa do mercado (MarketFeeBps) é descontada do vendedor na venda.
func (s *Service) ListItem(ctx context.Context, sellerID, itemType, itemID string, price int64) (string, error) {
	if price <= 0 {
		return "", fmt.Errorf("preço deve ser positivo")
	}
	if itemType != "trophy" && itemType != "equipment" {
		return "", fmt.Errorf("tipo de item inválido: %s", itemType)
	}
	fee := price * int64(s.MarketFeeBps) / 10000
	return repo.CreateListing(ctx, s.Pool, sellerID, itemType, itemID, price, fee)
}

// BrowseMarket lista os anúncios ativos (filtra por tipo se != "").
func (s *Service) BrowseMarket(ctx context.Context, itemType string) ([]repo.ListingView, error) {
	return repo.BrowseListings(ctx, s.Pool, itemType, 100)
}

// MyListings lista os anúncios ativos do jogador.
func (s *Service) MyListings(ctx context.Context, sellerID string) ([]repo.ListingView, error) {
	return repo.MyListings(ctx, s.Pool, sellerID)
}

// BuyItem compra um anúncio (transfere item + ouro numa transação).
func (s *Service) BuyItem(ctx context.Context, buyerID, listingID string) (*repo.ListingView, error) {
	v, err := repo.BuyListing(ctx, s.Pool, buyerID, listingID)
	if err == nil {
		// O ouro do comprador e do vendedor mudou — invalida o cache quente de
		// ambos (qualquer um pode estar pescando) para repovoar do Postgres.
		repo.CacheDelSession(ctx, s.Redis, buyerID)
		repo.CacheDelSession(ctx, s.Redis, v.SellerID)
	}
	return v, err
}

// CancelListing devolve o item ao vendedor e fecha o anúncio.
func (s *Service) CancelListing(ctx context.Context, sellerID, listingID string) error {
	return repo.CancelListing(ctx, s.Pool, sellerID, listingID)
}
