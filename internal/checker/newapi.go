package checker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NewAPIProber implements the NewAPI token format.
type NewAPIProber struct{}

func (p *NewAPIProber) Name() string { return "newapi_token" }

func (p *NewAPIProber) Probe(baseURL, apiKey, authType string) (*Result, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	url := baseURL + "/api/usage/token/"
	body, err := doRequest(client, url, apiKey, authType)
	if err != nil {
		return nil, fmt.Errorf("token usage request failed: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode token usage response: %w", err)
	}

	// Expect code:0 and data.total_available.
	code, ok := getFloat(resp, "code")
	if !ok || code != 0 {
		return nil, fmt.Errorf("unexpected response code or missing code field")
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing data field in response")
	}

	totalAvailable, ok := getFloat(data, "total_available")
	if !ok {
		return nil, fmt.Errorf("missing total_available in data")
	}

	extra := map[string]float64{"total_available_raw": totalAvailable}
	if granted, ok := getFloat(data, "total_granted"); ok {
		extra["total_granted"] = granted
	}
	if used, ok := getFloat(data, "total_used"); ok {
		extra["total_used"] = used
	}

	// New API: 500000 quota units = $1 USD
	balanceUSD := totalAvailable / 500000.0

	return &Result{
		Balance: balanceUSD,
		Unit:    "USD",
		Extra:   extra,
	}, nil
}
