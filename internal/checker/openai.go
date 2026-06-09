package checker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"upstream-balance/internal/model"
)

// OpenAIProber implements the OpenAI-compatible billing API format.
type OpenAIProber struct{}

func (p *OpenAIProber) Name() string { return "openai_compat" }

func (p *OpenAIProber) Probe(site *model.Site) (*Result, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	// GET /v1/dashboard/billing/subscription
	subURL := baseURL + "/v1/dashboard/billing/subscription"
	body, err := doRequest(client, subURL, site.APIKey, site.AuthType)
	if err != nil {
		return nil, fmt.Errorf("subscription request failed: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode subscription response: %w", err)
	}

	// Check for code:0 + data.balance format (some providers).
	if code, ok := getFloat(resp, "code"); ok && code == 0 {
		if data, ok := resp["data"].(map[string]interface{}); ok {
			if balance, ok := getFloat(data, "balance"); ok {
				return &Result{
					Balance: balance,
					Unit:    "USD",
				}, nil
			}
		}
	}

	// Standard OpenAI format: hard_limit_usd present.
	hardLimit, ok := getFloat(resp, "hard_limit_usd")
	if !ok {
		return nil, fmt.Errorf("no hard_limit_usd or data.balance in response")
	}

	// Query usage for the last 100 days.
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -100).Format("2006-01-02")
	endDate := now.AddDate(0, 0, 1).Format("2006-01-02")
	usageURL := fmt.Sprintf("%s/v1/dashboard/billing/usage?start_date=%s&end_date=%s",
		baseURL, startDate, endDate)

	usageBody, err := doRequest(client, usageURL, site.APIKey, site.AuthType)
	if err != nil {
		return nil, fmt.Errorf("usage request failed: %w", err)
	}

	var usageResp map[string]interface{}
	if err := json.Unmarshal(usageBody, &usageResp); err != nil {
		return nil, fmt.Errorf("failed to decode usage response: %w", err)
	}

	totalUsage, _ := getFloat(usageResp, "total_usage")
	// total_usage is in cents, convert to dollars.
	used := totalUsage / 100.0
	remaining := hardLimit - used

	extra := map[string]float64{
		"limit": hardLimit,
		"used":  used,
	}

	return &Result{
		Balance: remaining,
		Unit:    "USD",
		Extra:   extra,
	}, nil
}

// doRequest performs an authenticated GET request and returns the response body.
func doRequest(client *http.Client, url, apiKey, authType string) ([]byte, error) {
	if authType == "url_key" {
		if strings.Contains(url, "?") {
			url += "&key=" + apiKey
		} else {
			url += "?key=" + apiKey
		}
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if authType != "url_key" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return body, nil
}

// getFloat extracts a numeric value from a map by key, handling both
// json.Number and float64 types.
func getFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
