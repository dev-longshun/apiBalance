package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"quota-sentinel/internal/checker"
	"quota-sentinel/internal/model"
	"quota-sentinel/internal/store"
)

type CheckHandler struct {
	sites      *store.SiteStore
	thresholds *store.ThresholdStore
	checker    *checker.Checker
}

func NewCheckHandler(sites *store.SiteStore, thresholds *store.ThresholdStore, chk *checker.Checker) *CheckHandler {
	return &CheckHandler{
		sites:      sites,
		thresholds: thresholds,
		checker:    chk,
	}
}

func (h *CheckHandler) CheckSite(c *gin.Context) {
	id := c.Param("id")

	site, err := h.sites.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if site == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
		return
	}

	result := h.checker.Check(site)

	status := "ok"
	lastError := ""
	if result.Error != "" {
		status = "error"
		lastError = result.Error
	}

	h.sites.UpdateBalance(site.ID, result.Balance, result.Unit, result.DetectedType, status, lastError)

	// Evaluate thresholds.
	var alertsSent []model.AlertSent
	if result.Error == "" {
		thresholdList, err := h.thresholds.ListBySite(site.ID)
		if err == nil {
			for _, t := range thresholdList {
				if result.Balance < t.Amount {
					status = "low"
					alertsSent = append(alertsSent, model.AlertSent{
						Threshold:   t.Amount,
						MessageSent: false,
					})
				}
			}
		}
		// Update status to "low" if any threshold was triggered.
		if status == "low" {
			h.sites.UpdateBalance(site.ID, result.Balance, result.Unit, result.DetectedType, status, "")
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"balance":       result.Balance,
		"unit":          result.Unit,
		"detected_type": result.DetectedType,
		"extra":         result.Extra,
		"error":         result.Error,
		"status":        status,
		"alerts_sent":   alertsSent,
	})
}

func (h *CheckHandler) CheckAll(c *gin.Context) {
	sites, err := h.sites.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	count := len(sites)

	go func(siteList []model.Site) {
		if len(siteList) == 0 {
			return
		}
		results := h.checker.CheckWithConcurrency(siteList, 10)
		for _, site := range siteList {
			result, ok := results[site.ID]
			if !ok {
				continue
			}

			status := "ok"
			lastError := ""
			if result.Error != "" {
				status = "error"
				lastError = result.Error
				h.sites.UpdateBalance(site.ID, site.Balance, site.BalanceUnit, site.DetectedType, status, lastError)
				continue
			}

			h.sites.UpdateBalance(site.ID, result.Balance, result.Unit, result.DetectedType, status, "")

			// Evaluate thresholds.
			thresholdList, err := h.thresholds.ListBySite(site.ID)
			if err != nil {
				continue
			}
			for _, t := range thresholdList {
				if result.Balance < t.Amount {
					status = "low"
					break
				}
			}
			if status == "low" {
				h.sites.UpdateBalance(site.ID, result.Balance, result.Unit, result.DetectedType, status, "")
			}
		}
	}(sites)

	c.JSON(http.StatusOK, gin.H{
		"message":    "全量查询已启动",
		"site_count": count,
	})
}
