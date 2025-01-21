package autoapprovers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"

	// Import Tailscale's ACLAutoApprovers type:
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// RegisterRoutes wires up auto-approver routes under /autoapprovers.
//
// For example:
//
//	GET    /autoapprovers       => retrieve the entire ACLAutoApprovers struct
//	POST   /autoapprovers       => create or set it (if none exists)
//	PUT    /autoapprovers       => update it (if one exists)
//	DELETE /autoapprovers       => remove it from the state
func RegisterRoutes(r *gin.Engine, state *common.State) {
	a := r.Group("/autoapprovers")
	{
		a.GET("", func(c *gin.Context) {
			getAutoApprovers(c, state)
		})
		a.POST("", func(c *gin.Context) {
			createAutoApprovers(c, state)
		})
		a.PUT("", func(c *gin.Context) {
			updateAutoApprovers(c, state)
		})
		a.DELETE("", func(c *gin.Context) {
			deleteAutoApprovers(c, state)
		})
	}
}

// getAutoApprovers => GET /autoapprovers
// Returns the entire struct if present, otherwise an empty object or 404.
func getAutoApprovers(c *gin.Context, state *common.State) {
	aap, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse autoApprovers"})
		return
	}
	if aap == nil {
		// If you prefer 404, do: c.JSON(http.StatusNotFound, ...)
		c.JSON(http.StatusOK, tsclient.ACLAutoApprovers{
			Routes:   map[string][]string{},
			ExitNode: []string{},
		})
		return
	}
	c.JSON(http.StatusOK, aap)
}

// createAutoApprovers => POST /autoapprovers
// Creates a new ACLAutoApprovers if none exists. If one already exists,
// you can either overwrite or return an error, depending on your preference.
func createAutoApprovers(c *gin.Context, state *common.State) {
	var newAAP tsclient.ACLAutoApprovers
	if err := c.ShouldBindJSON(&newAAP); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing autoApprovers"})
		return
	}

	// If you want to error out if there's already one:
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "autoApprovers already exists"})
		return
	}

	// Overwrite or create:
	if err := state.UpdateKeyAndSave("autoApprovers", newAAP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save autoApprovers"})
		return
	}
	c.JSON(http.StatusCreated, newAAP)
}

// updateAutoApprovers => PUT /autoapprovers
// Updates an existing struct. If none exists, you might return 404 or create it.
func updateAutoApprovers(c *gin.Context, state *common.State) {
	var updated tsclient.ACLAutoApprovers
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse autoApprovers"})
		return
	}
	if existing == nil {
		// If you prefer to create if not existing, do that:
		// or error out:
		c.JSON(http.StatusNotFound, gin.H{"error": "No autoApprovers found to update"})
		return
	}

	// Overwrite the stored object
	if err := state.UpdateKeyAndSave("autoApprovers", updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update autoApprovers"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteAutoApprovers => DELETE /autoapprovers
// Remove it from state. If none exists, return 404 or no-op.
func deleteAutoApprovers(c *gin.Context, state *common.State) {
	existing, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse autoApprovers"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No autoApprovers found"})
		return
	}

	// Remove the key entirely:
	if err := state.UpdateKeyAndSave("autoApprovers", nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete autoApprovers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "autoApprovers deleted"})
}

// getAutoApproversFromState re-marshal state.Data["autoapprovers"] to *tsclient.ACLAutoApprovers
func getAutoApproversFromState(state *common.State) (*tsclient.ACLAutoApprovers, error) {
	raw := state.GetValue("autoApprovers")
	if raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var aap tsclient.ACLAutoApprovers
	if err := json.Unmarshal(b, &aap); err != nil {
		return nil, err
	}
	return &aap, nil
}
