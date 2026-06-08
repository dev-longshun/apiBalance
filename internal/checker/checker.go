package checker

import (
	"fmt"
	"sync"

	"quota-sentinel/internal/model"
)

// Prober interface - each API format implements this.
type Prober interface {
	Name() string
	Probe(baseURL, apiKey, authType string) (*Result, error)
}

// Result holds the outcome of a single successful probe.
type Result struct {
	Balance float64
	Unit    string
	Extra   map[string]float64
}

// Checker orchestrates sequential probing across registered Probers.
type Checker struct {
	probers []Prober
}

// New creates a Checker with the four probers registered in priority order.
func New() *Checker {
	return &Checker{
		probers: []Prober{
			&OpenAIProber{},
			&Sub2APIProber{},
			&AuthMeProber{},
			&NewAPIProber{},
		},
	}
}

// Check probes a single site. If cachedType matches a registered prober name,
// that prober is tried first. Otherwise (or on cached miss) all probers are
// tried in registration order. The first success wins.
func (c *Checker) Check(baseURL, apiKey, authType, cachedType string) *model.CheckResult {
	// If we have a cached type, try that prober first.
	if cachedType != "" {
		for _, p := range c.probers {
			if p.Name() == cachedType {
				res, err := p.Probe(baseURL, apiKey, authType)
				if err == nil && res != nil {
					return &model.CheckResult{
						Balance:      res.Balance,
						Unit:         res.Unit,
						DetectedType: p.Name(),
						Extra:        res.Extra,
					}
				}
				break
			}
		}
	}

	// Try all probers in order.
	var lastErr string
	for _, p := range c.probers {
		res, err := p.Probe(baseURL, apiKey, authType)
		if err != nil {
			lastErr = fmt.Sprintf("%s: %v", p.Name(), err)
			continue
		}
		if res != nil {
			return &model.CheckResult{
				Balance:      res.Balance,
				Unit:         res.Unit,
				DetectedType: p.Name(),
				Extra:        res.Extra,
			}
		}
	}

	// All probers failed.
	if lastErr == "" {
		lastErr = "all probers returned nil"
	}
	return &model.CheckResult{
		Error: lastErr,
	}
}

// CheckWithConcurrency checks multiple sites concurrently, limited by maxConcurrency.
func (c *Checker) CheckWithConcurrency(sites []model.Site, maxConcurrency int) map[string]*model.CheckResult {
	results := make(map[string]*model.CheckResult, len(sites))
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, maxConcurrency)

	for _, site := range sites {
		wg.Add(1)
		go func(s model.Site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cr := c.Check(s.BaseURL, s.APIKey, s.AuthType, s.DetectedType)
			mu.Lock()
			results[s.ID] = cr
			mu.Unlock()
		}(site)
	}

	wg.Wait()
	return results
}
