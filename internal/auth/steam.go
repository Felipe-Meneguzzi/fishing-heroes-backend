package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// SteamVerifier valida um ticket de sessão Steam e devolve o SteamID64.
type SteamVerifier interface {
	Verify(ctx context.Context, ticket string) (steamID string, err error)
}

// DevSteamVerifier — APENAS para desenvolvimento: trata o "ticket" como o
// próprio SteamID, permitindo logar sem o cliente Steam real.
type DevSteamVerifier struct{}

func (DevSteamVerifier) Verify(_ context.Context, ticket string) (string, error) {
	if ticket == "" {
		return "", fmt.Errorf("steamId (ticket dev) vazio")
	}
	return ticket, nil
}

// WebAPISteamVerifier — produção: valida o ticket via Steam Web API
// (ISteamUserAuth/AuthenticateUserTicket). Requer Web API key + AppID.
type WebAPISteamVerifier struct {
	APIKey string
	AppID  string
	HTTP   *http.Client
}

func NewWebAPISteamVerifier(apiKey, appID string) *WebAPISteamVerifier {
	return &WebAPISteamVerifier{APIKey: apiKey, AppID: appID, HTTP: &http.Client{Timeout: 8 * time.Second}}
}

func (v *WebAPISteamVerifier) Verify(ctx context.Context, ticket string) (string, error) {
	q := url.Values{
		"key":    {v.APIKey},
		"appid":  {v.AppID},
		"ticket": {ticket}, // ticket de autenticação em hex
	}
	endpoint := "https://api.steampowered.com/ISteamUserAuth/AuthenticateUserTicket/v1/?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("steam web api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("steam web api status %d", resp.StatusCode)
	}

	var out struct {
		Response struct {
			Params struct {
				Result  string `json:"result"`
				SteamID string `json:"steamid"`
			} `json:"params"`
			Error *struct {
				ErrorCode int    `json:"errorcode"`
				ErrorDesc string `json:"errordesc"`
			} `json:"error"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("steam web api: resposta inválida: %w", err)
	}
	if out.Response.Error != nil {
		return "", fmt.Errorf("steam recusou o ticket: %s", out.Response.Error.ErrorDesc)
	}
	if out.Response.Params.Result != "OK" || out.Response.Params.SteamID == "" {
		return "", fmt.Errorf("ticket Steam inválido")
	}
	return out.Response.Params.SteamID, nil
}
