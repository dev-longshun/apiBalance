package checker

import (
	"fmt"
	"sync"

	"upstream-balance/internal/model"
)

// Prober interface - each API format implements this.
type Prober interface {
	Name() string
	Probe(site *model.Site) (*Result, error)
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
	newapi  *NewAPIProber
}

// New creates a Checker with the four probers registered in priority order.
func New() *Checker {
	newapi := &NewAPIProber{}
	return &Checker{
		probers: []Prober{
			newapi,
			&Sub2APIProber{},
			&AuthMeProber{},
			&OpenAIProber{},
		},
		newapi: newapi,
	}
}

// Check probes a single site. If cachedType matches a registered prober name,
// that prober is tried first. Otherwise (or on cached miss) all probers are
// tried in registration order. The first success wins.
func (c *Checker) Check(site *model.Site) *model.CheckResult {
	probers := c.probers
	// Panel credentials (access token or username+password) only exist on
	// NewAPI, and the other three probers all need an API key — running them
	// would just bury the real NewAPI error under a bogus HTTP 401.
	if site.AccessToken != "" || (site.Username != "" && site.Password != "") {
		probers = []Prober{c.newapi}
	}

	// If we have a cached type, try that prober first.
	if site.DetectedType != "" {
		for _, p := range probers {
			if p.Name() == site.DetectedType {
				res, err := p.Probe(site)
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
	for _, p := range probers {
		res, err := p.Probe(site)
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

			cr := c.Check(&s)
			mu.Lock()
			results[s.ID] = cr
			mu.Unlock()
		}(site)
	}

	wg.Wait()
	return results
}
