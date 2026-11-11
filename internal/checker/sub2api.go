package checker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"upstream-balance/internal/model"
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
		// 累计实际扣费只增不减，充值不影响它，算一段时间的消耗比余额差更准
		if total, ok := usage["total"].(map[string]interface{}); ok {
			if cost, ok := actualCost(total); ok {
				extra["total_cost"] = cost
			}
		}
	}

	result := &Result{
		Balance: balance,
		Unit:    "USD",
		Daily:   dailyUsage(resp),
	}
	if len(extra) > 0 {
		result.Extra = extra
	}
	return result, nil
}

// actualCost 取实际扣费（actual_cost，已乘站点倍率）；老版本 sub2api 没有这个字段时退回 cost。
func actualCost(m map[string]interface{}) (float64, bool) {
	if v, ok := getFloat(m, "actual_cost"); ok {
		return v, true
	}
	return getFloat(m, "cost")
}

// dailyUsage 解析 daily_usage：[{date, actual_cost, ...}]，没有或格式不对就返回 nil。
func dailyUsage(resp map[string]interface{}) []model.DailyCost {
	list, ok := resp["daily_usage"].([]interface{})
	if !ok {
		return nil
	}
	var out []model.DailyCost
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		date, _ := m["date"].(string)
		cost, ok := actualCost(m)
		if len(date) < 10 || !ok {
			continue
		}
		out = append(out, model.DailyCost{Date: date[:10], Cost: cost})
	}
	return out
}
