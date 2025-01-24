package settings

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse is used for error documentation in swagger.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Settings represents a simple configuration with three fields.
//
// @Description Settings define certain toggles or values controlling features like IPv4 usage, NAT routing, etc.
type Settings struct {
	// DisableIPv4 indicates whether IPv4 traffic is disabled.
	DisableIPv4 bool `json:"disableIPv4,omitempty" hujson:"DisableIPv4,omitempty"`
	// OneCGNATRoute can store a special CGNAT route (e.g., "100.64.0.0/10").
	OneCGNATRoute string `json:"oneCGNATRoute,omitempty" hujson:"OneCGNATRoute,omitempty"`
	// RandomizeClientPort indicates whether to use a random local port instead of a fixed one.
	RandomizeClientPort bool `json:"randomizeClientPort,omitempty" hujson:"RandomizeClientPort,omitempty"`
}

// RegisterRoutes wires up the single-resource Settings at /settings.
//
//   GET    /settings => retrieve the current settings
//   POST   /settings => create new settings if none exist
//   PUT    /settings => update existing settings
//   DELETE /settings => remove the settings entirely
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
// @Summary      Retrieve the settings
// @Description  Returns the current settings or an empty struct if none exist.
// @Tags         Settings
// @Accept       json
// @Produce      json
// @Success      200 {object} Settings
// @Failure      500 {object} ErrorResponse "Failed to parse settings"
// @Router       /settings [get]
func getSettings(c *gin.Context, state *common.State) {
	cfg, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse settings"})
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
// @Summary      Create new settings
// @Description  Creates a new Settings object if none exist; returns 409 if one already exists.
// @Tags         Settings
// @Accept       json
// @Produce      json
// @Param        settings body Settings true "Settings to create"
// @Success      201 {object} Settings
// @Failure      400 {object} ErrorResponse "Invalid JSON body"
// @Failure      409 {object} ErrorResponse "Settings already exist"
// @Failure      500 {object} ErrorResponse "Failed to check or save settings"
// @Router       /settings [post]
func createSettings(c *gin.Context, state *common.State) {
	var newCfg Settings
	if err := c.ShouldBindJSON(&newCfg); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	existing, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to check existing settings"})
		return
	}
	if existing != nil {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "Settings already exist"})
		return
	}

	if err := state.UpdateKeyAndSave("settings", newCfg); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new settings"})
		return
	}
	c.JSON(http.StatusCreated, newCfg)
}

// updateSettings => PUT /settings
// @Summary      Update existing settings
// @Description  Updates the current settings. Returns 404 if none exist.
// @Tags         Settings
// @Accept       json
// @Produce      json
// @Param        settings body Settings true "Updated settings"
// @Success      200 {object} Settings
// @Failure      400 {object} ErrorResponse "Invalid JSON body"
// @Failure      404 {object} ErrorResponse "No existing settings to update"
// @Failure      500 {object} ErrorResponse "Failed to update settings"
// @Router       /settings [put]
func updateSettings(c *gin.Context, state *common.State) {
	var updated Settings
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	existing, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to check existing settings"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No existing settings to update"})
		return
	}

	if err := state.UpdateKeyAndSave("settings", updated); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update settings"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteSettings => DELETE /settings
// @Summary      Delete settings
// @Description  Removes the current settings if present; returns 404 if none exist.
// @Tags         Settings
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]string "Settings deleted"
// @Failure      404 {object} ErrorResponse "No existing settings found to delete"
// @Failure      500 {object} ErrorResponse "Failed to delete settings"
// @Router       /settings [delete]
func deleteSettings(c *gin.Context, state *common.State) {
	existing, err := getSettingsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to check existing settings"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No existing settings found to delete"})
		return
	}

	if err := state.UpdateKeyAndSave("settings", nil); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Settings deleted"})
}

// getSettingsFromState => re-marshal state.Data["settings"] to *Settings
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
