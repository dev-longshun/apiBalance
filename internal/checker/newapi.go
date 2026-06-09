package checker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"upstream-balance/internal/model"
)

// NewAPIProber implements the NewAPI/OneAPI balance query.
// When username+password are configured, it logs in and queries /api/user/self
// (the accurate user balance). Otherwise it falls back to the token-based
// /api/usage/token/ endpoint.
type NewAPIProber struct{}

func (p *NewAPIProber) Name() string { return "newapi_token" }

func (p *NewAPIProber) Probe(site *model.Site) (*Result, error) {
	if site.Username != "" && site.Password != "" {
		return p.probeViaLogin(site)
	}
	if site.APIKey != "" {
		return p.probeViaToken(site)
	}
	return nil, fmt.Errorf("no credentials configured (need username+password or api_key)")
}

// probeViaLogin authenticates with username/password, then queries /api/user/self.
// This returns the user's actual remaining quota — the correct balance.
func (p *NewAPIProber) probeViaLogin(site *model.Site) (*Result, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Step 1: Login
	sessionCookie, loginUserID, err := p.login(client, baseURL, site.Username, site.Password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	// Step 2: Resolve userId — use configured value if > 0, otherwise login userId
	resolvedUserID := loginUserID
	if site.UserID > 0 {
		resolvedUserID = site.UserID
	}

	// Step 3: Query /api/user/self
	quota, usedQuota, err := p.fetchUserSelf(client, baseURL, sessionCookie, resolvedUserID)
	if err != nil {
		return nil, fmt.Errorf("fetch user/self failed: %w", err)
	}

	balanceUSD := float64(quota) / 500000.0

	extra := map[string]float64{
		"quota_raw":      float64(quota),
		"used_quota_raw": float64(usedQuota),
	}

	return &Result{
		Balance: balanceUSD,
		Unit:    "USD",
		Extra:   extra,
	}, nil
}

func (p *NewAPIProber) login(client *http.Client, baseURL, username, password string) (string, int, error) {
	payload, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/user/login", bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var loginResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    *struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", 0, fmt.Errorf("decode login response: %w", err)
	}
	if !loginResp.Success || loginResp.Data == nil {
		return "", 0, fmt.Errorf("%s", loginResp.Message)
	}

	// Extract session cookie from Set-Cookie header.
	var sessionCookie string
	re := regexp.MustCompile(`session=([^;]+)`)
	for _, c := range resp.Header.Values("Set-Cookie") {
		if m := re.FindStringSubmatch(c); len(m) == 2 {
			sessionCookie = m[1]
			break
		}
	}
	if sessionCookie == "" {
		return "", 0, fmt.Errorf("no session cookie in response")
	}

	return sessionCookie, loginResp.Data.ID, nil
}

func (p *NewAPIProber) fetchUserSelf(client *http.Client, baseURL, sessionCookie string, userID int) (quota int64, usedQuota int64, err error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/user/self", nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Cookie", fmt.Sprintf("session=%s", sessionCookie))
	req.Header.Set("New-Api-User", fmt.Sprintf("%d", userID))

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var selfResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    *struct {
			Quota     int64 `json:"quota"`
			UsedQuota int64 `json:"used_quota"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&selfResp); err != nil {
		return 0, 0, fmt.Errorf("decode user/self response: %w", err)
	}
	if !selfResp.Success || selfResp.Data == nil {
		return 0, 0, fmt.Errorf("%s", selfResp.Message)
	}

	return selfResp.Data.Quota, selfResp.Data.UsedQuota, nil
}

// probeViaToken is the legacy fallback: query /api/usage/token/ with an API key.
func (p *NewAPIProber) probeViaToken(site *model.Site) (*Result, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	url := baseURL + "/api/usage/token/"
	body, err := doRequest(client, url, site.APIKey, site.AuthType)
	if err != nil {
		return nil, fmt.Errorf("token usage request failed: %w", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode token usage response: %w", err)
	}

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

	balanceUSD := totalAvailable / 500000.0

	return &Result{
		Balance: balanceUSD,
		Unit:    "USD",
		Extra:   extra,
	}, nil
}
