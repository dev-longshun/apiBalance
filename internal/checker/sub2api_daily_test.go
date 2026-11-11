package checker

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"upstream-balance/internal/model"
)

func TestSub2APIParsesDailyAndTotalCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"balance":432.18,"usage":{"today":{"cost":20.2,"actual_cost":2.9},
			"total":{"cost":831.9,"actual_cost":121.5}},
			"daily_usage":[{"date":"2026-11-10","cost":80,"actual_cost":12.1},{"date":"2026-11-11T00:00:00+08:00","cost":20}]}`))
	}))
	defer srv.Close()

	res, err := (&Sub2APIProber{}).Probe(&model.Site{BaseURL: srv.URL, APIKey: "k", AuthType: "bearer"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["total_cost"] != 121.5 {
		t.Fatalf("total_cost = %v, want actual_cost 121.5", res.Extra["total_cost"])
	}
	want := []model.DailyCost{{Date: "2026-11-10", Cost: 12.1}, {Date: "2026-11-11", Cost: 20}}
	if len(res.Daily) != 2 || res.Daily[0] != want[0] || res.Daily[1] != want[1] {
		t.Fatalf("daily = %+v", res.Daily)
	}
}
