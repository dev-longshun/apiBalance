package checker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"quota-sentinel/internal/model"
)

// Sub2APIProber implements the sub2api usage format.
type Sub2APIProber struct{}

func (p *Sub2APIProber) Name() string { return "sub2api" }

func (p *Sub2APIProber) Probe(site *model.Site) (*Result, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	url := baseURL + "/v1/usage"
	body, err := doRequest(client, url, site.APIKey, site.AuthType)
	if err != nil {
		return nil, fmt.Errorf("usage request failed: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode usage response: %w", err)
	}

	// Try "balance" first, then "remaining".
	balance, found := getFloat(resp, "balance")
	if !found {
		balance, found = getFloat(resp, "remaining")
	}
	if !found {
		return nil, fmt.Errorf("no balance or remaining field in response")
	}

	extra := make(map[string]float64)

	// Extract usage.today.cost if present.
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		if today, ok := usage["today"].(map[string]interface{}); ok {
			if cost, ok := getFloat(today, "cost"); ok {
				extra["today_cost"] = cost
			}
		}
	}

	result := &Result{
		Balance: balance,
		Unit:    "USD",
	}
	if len(extra) > 0 {
		result.Extra = extra
	}
	return result, nil
}
