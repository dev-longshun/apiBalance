package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"quota-sentinel/internal/checker"
	"quota-sentinel/internal/model"
	"quota-sentinel/internal/store"
)

type SiteHandler struct {
	sites      *store.SiteStore
	thresholds *store.ThresholdStore
	checker    *checker.Checker
}

func NewSiteHandler(sites *store.SiteStore, thresholds *store.ThresholdStore, chk *checker.Checker) *SiteHandler {
	return &SiteHandler{
		sites:      sites,
		thresholds: thresholds,
		checker:    chk,
	}
}

func (h *SiteHandler) List(c *gin.Context) {
	sites, err := h.sites.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for i := range sites {
		amounts, err := h.thresholds.GetAmountsBySite(sites[i].ID)
		if err == nil {
			sites[i].Thresholds = amounts
		}
		sites[i].MaskKey()
	}

	c.JSON(http.StatusOK, sites)
}

func (h *SiteHandler) Create(c *gin.Context) {
	var req model.SiteCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.AuthType == "" {
		req.AuthType = "bearer"
	}
	if len(req.Thresholds) == 0 {
		req.Thresholds = []float64{10}
	}

	now := model.Now()
	site := &model.Site{
		ID:        uuid.New().String(),
		Name:      req.Name,
		BaseURL:   req.BaseURL,
		APIKey:    req.APIKey,
		AuthType:  req.AuthType,
		Status:    "unknown",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.sites.Create(site); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.thresholds.ReplaceBySite(site.ID, req.Thresholds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	site.Thresholds = req.Thresholds

	// Trigger async balance check.
	go func(s model.Site) {
		result := h.checker.Check(s.BaseURL, s.APIKey, s.AuthType, "")
		status := "ok"
		lastError := ""
		if result.Error != "" {
			status = "error"
			lastError = result.Error
		} else {
			thresholds, err := h.thresholds.GetAmountsBySite(s.ID)
			if err == nil {
				for _, t := range thresholds {
					if result.Balance < t {
						status = "low"
						break
					}
				}
			}
		}
		h.sites.UpdateBalance(s.ID, result.Balance, result.Unit, result.DetectedType, status, lastError)
	}(*site)

	site.MaskKey()
	c.JSON(http.StatusCreated, site)
}

func (h *SiteHandler) Update(c *gin.Context) {
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

	var req model.SiteUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})
	clearDetected := false

	if req.Name != nil {
		updates["name"] = *req.Name
		site.Name = *req.Name
	}
	if req.BaseURL != nil {
		updates["base_url"] = *req.BaseURL
		site.BaseURL = *req.BaseURL
		clearDetected = true
	}
	if req.APIKey != nil {
		updates["api_key"] = *req.APIKey
		site.APIKey = *req.APIKey
		clearDetected = true
	}
	if req.AuthType != nil {
		updates["auth_type"] = *req.AuthType
		site.AuthType = *req.AuthType
	}
	if clearDetected {
		updates["detected_type"] = ""
		site.DetectedType = ""
	}
	updates["updated_at"] = model.Now()
	site.UpdatedAt = updates["updated_at"].(string)

	if err := h.sites.Update(id, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Thresholds != nil {
		if err := h.thresholds.ReplaceBySite(id, *req.Thresholds); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		site.Thresholds = *req.Thresholds
	} else {
		amounts, err := h.thresholds.GetAmountsBySite(id)
		if err == nil {
			site.Thresholds = amounts
		}
	}

	site.MaskKey()
	c.JSON(http.StatusOK, site)
}

func (h *SiteHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.sites.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
		return
	}

	c.Status(http.StatusNoContent)
}
