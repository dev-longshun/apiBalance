package checker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"upstream-balance/internal/model"
)

// AuthMeProber implements the JWT/auth me format.
type AuthMeProber struct{}

func (p *AuthMeProber) Name() string { return "auth_me" }

func (p *AuthMeProber) Probe(site *model.Site) (*Result, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	url := baseURL + "/api/v1/auth/me"
	body, err := doRequest(client, url, site.APIKey, site.AuthType)
	if err != nil {
		return nil, fmt.Errorf("auth/me request failed: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode auth/me response: %w", err)
	}

	// Expect code:0 and data.balance.
	code, ok := getFloat(resp, "code")
	if !ok || code != 0 {
		return nil, fmt.Errorf("unexpected response code or missing code field")
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing data field in response")
	}

	balance, ok := getFloat(data, "balance")
	if !ok {
		return nil, fmt.Errorf("missing balance in data")
	}

	return &Result{
		Balance: balance,
		Unit:    "USD",
	}, nil
}
