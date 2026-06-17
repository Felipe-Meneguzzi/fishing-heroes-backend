// Package auth — emissão/validação de tokens de sessão e verificação de
// identidade Steam. As contas do jogo são vinculadas ao SteamID.
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims — conteúdo do token de sessão.
type Claims struct {
	PlayerID  string `json:"pid"`
	AccountID string `json:"aid"`
	SteamID   string `json:"sid"`
	jwt.RegisteredClaims
}

// TokenManager assina e valida tokens HMAC (HS256).
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

// Issue emite um token para o jogador/conta.
func (m *TokenManager) Issue(playerID, accountID, steamID string) (string, error) {
	now := time.Now()
	c := Claims{
		PlayerID:  playerID,
		AccountID: accountID,
		SteamID:   steamID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   playerID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
}

// Verify valida a assinatura/expiração e devolve os claims.
func (m *TokenManager) Verify(token string) (*Claims, error) {
	c := &Claims{}
	_, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}
