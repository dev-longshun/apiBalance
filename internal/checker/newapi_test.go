package checker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"upstream-balance/internal/model"
)

// newAPIStub mimics NewAPI's /api/user/self: the access token is matched
// against the New-Api-User header, exactly like middleware.authHelper does.
func newAPIStub(t *testing.T, wantToken string, wantUserID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/self" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != wantToken {
			json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "无权进行此操作，access token 无效"})
			return
		}
		if r.Header.Get("New-Api-User") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "无权进行此操作，未提供用户 ID"})
			return
		}
		if r.Header.Get("New-Api-User") != wantUserID {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "无权进行此操作，用户 ID 不匹配"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"quota": 12500000, "used_quota": 2500000},
		})
	}))
}

func TestNewAPIProbeViaAccessToken(t *testing.T) {
	srv := newAPIStub(t, "abc123token", "7")
	defer srv.Close()

	res := New().Check(&model.Site{BaseURL: srv.URL, AccessToken: "abc123token", UserID: 7})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Balance != 25 { // 12500000 / 500000
		t.Errorf("balance = %v, want 25", res.Balance)
	}
	if res.Unit != "USD" || res.DetectedType != "newapi_token" {
		t.Errorf("unit/type = %q/%q", res.Unit, res.DetectedType)
	}
	if res.Extra["used_quota_raw"] != 2500000 {
		t.Errorf("used_quota_raw = %v, want 2500000", res.Extra["used_quota_raw"])
	}
}

// A trailing slash on the base URL must not double up in the request path.
func TestNewAPIProbeViaAccessTokenTrailingSlash(t *testing.T) {
	srv := newAPIStub(t, "abc123token", "7")
	defer srv.Close()

	res := New().Check(&model.Site{BaseURL: srv.URL + "/", AccessToken: "abc123token", UserID: 7})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Balance != 25 {
		t.Errorf("balance = %v, want 25", res.Balance)
	}
}

// Without a user id NewAPI rejects the token; the error must say so instead of
// being buried under a bogus openai_compat HTTP 401.
func TestNewAPIProbeViaAccessTokenMissingUserID(t *testing.T) {
	srv := newAPIStub(t, "abc123token", "7")
	defer srv.Close()

	res := New().Check(&model.Site{BaseURL: srv.URL, AccessToken: "abc123token"})
	if res.Error == "" {
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(res.Error, "newapi_token:") || !strings.Contains(res.Error, "User ID") {
		t.Errorf("error = %q, want a newapi_token error mentioning User ID", res.Error)
	}
}

// The access token wins over stale username/password on the same site.
func TestNewAPIAccessTokenPreferredOverLogin(t *testing.T) {
	srv := newAPIStub(t, "abc123token", "7")
	defer srv.Close()

	res := New().Check(&model.Site{
		BaseURL:     srv.URL,
		AccessToken: "abc123token",
		UserID:      7,
		Username:    "admin",
		Password:    "wrong",
	})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Balance != 25 {
		t.Errorf("balance = %v, want 25", res.Balance)
	}
}

func TestSiteMaskSecretsHidesAccessToken(t *testing.T) {
	s := &model.Site{APIKey: "sk-abcdefgh", AccessToken: "abc123token", Password: "pw"}
	s.MaskSecrets()
	if s.AccessToken != "" || s.Password != "" || s.APIKey != "" {
		t.Fatalf("secrets leaked: %+v", s)
	}
	if s.AccessTokenMasked != "abc123***" {
		t.Errorf("AccessTokenMasked = %q, want abc123***", s.AccessTokenMasked)
	}
}
