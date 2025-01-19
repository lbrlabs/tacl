package settings

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// Settings represents a simple configuration with three fields.
type Settings struct {
	DisableIPv4         bool   `json:"disableIPv4,omitempty"       hujson:"DisableIPv4,omitempty"`
	OneCGNATRoute       string `json:"oneCGNATRoute,omitempty"     hujson:"OneCGNATRoute,omitempty"`
	RandomizeClientPort bool   `json:"randomizeClientPort,omitempty" hujson:"RandomizeClientPort,omitempty"`
}

// RegisterRoutes wires up the single-resource Settings at /settings.
func RegisterRoutes(r *gin.Engine, state *common.State) {
	s := r.Group("/settings")
	{
		s.GET("", func(c *gin.Context) {
			getSettings(c, state)
		})
		s.POST("", func(c *gin.Context) {
			createSettings(c, state)
		})
		s.PUT("", func(c *gin.Context) {
			updateSettings(c, state)
		})
		s.DELETE("", func(c *gin.Context) {
			deleteSettings(c, state)
		})
	}
}

// getSettings => GET /settings
// Returns the current settings or an empty default if none.
func getSettings(c *gin.Context, state *common.State) {
	cfg, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse settings"})
		return
	}
	if cfg == nil {
		// Return an empty struct if you prefer. Or 404 if you'd rather.
		c.JSON(http.StatusOK, Settings{})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// createSettings => POST /settings
// Creates a new Settings object if none exist; else 409 if already present.
func createSettings(c *gin.Context, state *common.State) {
	var newCfg Settings
	if err := c.ShouldBindJSON(&newCfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing settings"})
		return
	}
	if existing != nil {
		// We already have settings => "duplicate instance"
		c.JSON(http.StatusConflict, gin.H{"error": "Settings already exist"})
		return
	}

	// Create it
	if err := state.UpdateKeyAndSave("settings", newCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new settings"})
		return
	}
	c.JSON(http.StatusCreated, newCfg)
}

// updateSettings => PUT /settings
// Updates the existing settings, or 404 if none exist yet.
func updateSettings(c *gin.Context, state *common.State) {
	var updated Settings
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing settings"})
		return
	}
	if existing == nil {
		// No settings yet => can't update
		c.JSON(http.StatusNotFound, gin.H{"error": "No existing settings to update"})
		return
	}

	if err := state.UpdateKeyAndSave("settings", updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteSettings => DELETE /settings
// Removes the settings if present; else 404 if none exist.
func deleteSettings(c *gin.Context, state *common.State) {
	existing, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing settings"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No existing settings found to delete"})
		return
	}

	// Remove from state
	if err := state.UpdateKeyAndSave("settings", nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Settings deleted"})
}

// getSettingsFromState => re-marshal state.Data["settings"] to *Settings.
func getSettingsFromState(state *common.State) (*Settings, error) {
	raw := state.GetValue("settings")
	if raw == nil {
		return nil, nil
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var cfg Settings
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
