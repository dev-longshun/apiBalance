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

// quotaPerUSD is NewAPI's QuotaPerUnit — raw quota units per US dollar.
const quotaPerUSD = 500000.0

// NewAPIProber implements the NewAPI/OneAPI balance query.
// Credentials are tried in order of preference:
//  1. access token + user id — NewAPI "系统访问令牌", queries /api/user/self directly;
//  2. username + password — logs in for a session cookie, then /api/user/self;
//  3. api key — the legacy token-scoped /api/usage/token/ endpoint.
//
// The access token is preferred: no panel password is stored, it can be revoked
// from the panel alone, and it side-steps the login flow entirely (which may be
// gated by Turnstile or 2FA and is rate-limited).
type NewAPIProber struct{}

func (p *NewAPIProber) Name() string { return "newapi_token" }

func (p *NewAPIProber) Probe(site *model.Site) (*Result, error) {
	if site.AccessToken != "" {
		return p.probeViaAccessToken(site)
	}
	if site.Username != "" && site.Password != "" {
		return p.probeViaLogin(site)
	}
	if site.APIKey != "" {
		return p.probeViaToken(site)
	}
	return nil, fmt.Errorf("no credentials configured (need access_token+user_id, username+password, or api_key)")
}

// probeViaAccessToken queries /api/user/self with the panel access token.
// NewAPI matches the token against the New-Api-User header, so user_id must be
// configured; OneAPI-style panels accept the token alone, so the header is only
// sent when a user id is set.
func (p *NewAPIProber) probeViaAccessToken(site *model.Site) (*Result, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	client := &http.Client{Timeout: 15 * time.Second}

	quota, usedQuota, err := p.fetchUserSelf(client, baseURL, accessTokenAuth(site.AccessToken), site.UserID)
	if err != nil {
		if site.UserID <= 0 {
			return nil, fmt.Errorf("fetch user/self failed: %w (NewAPI access token also needs User ID)", err)
		}
		return nil, fmt.Errorf("fetch user/self failed: %w", err)
	}

	return newAPIQuotaResult(quota, usedQuota), nil
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
	quota, usedQuota, err := p.fetchUserSelf(client, baseURL, sessionAuth(sessionCookie), resolvedUserID)
	if err != nil {
		return nil, fmt.Errorf("fetch user/self failed: %w", err)
	}

	return newAPIQuotaResult(quota, usedQuota), nil
}

// newAPIQuotaResult converts raw NewAPI quota units into a USD balance.
func newAPIQuotaResult(quota, usedQuota int64) *Result {
	return &Result{
		Balance: float64(quota) / quotaPerUSD,
		Unit:    "USD",
		Extra: map[string]float64{
			"quota_raw":      float64(quota),
			"used_quota_raw": float64(usedQuota),
		},
	}
}

// selfAuth applies one credential flavour to a /api/user/self request.
type selfAuth func(req *http.Request)

func sessionAuth(cookie string) selfAuth {
	return func(req *http.Request) {
		req.Header.Set("Cookie", fmt.Sprintf("session=%s", cookie))
	}
}

// accessTokenAuth sends the raw access token; NewAPI strips an optional
// "Bearer " prefix itself, so the bare value works on both NewAPI and OneAPI.
func accessTokenAuth(token string) selfAuth {
	return func(req *http.Request) {
		req.Header.Set("Authorization", token)
	}
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

func (p *NewAPIProber) fetchUserSelf(client *http.Client, baseURL string, auth selfAuth, userID int) (quota int64, usedQuota int64, err error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/user/self", nil)
	if err != nil {
		return 0, 0, err
	}
	auth(req)
	if userID > 0 {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", userID))
	}

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

	balanceUSD := totalAvailable / quotaPerUSD

	return &Result{
		Balance: balanceUSD,
		Unit:    "USD",
		Extra:   extra,
	}, nil
}
