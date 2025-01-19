package derpmap

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"

	// If ACLDERPMap and ACLDERPRegion are from tailscale-client-go:
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// RegisterRoutes wires up the /derpmap endpoints.
// We'll store the config in state.Data["derpMap"] as a single object.
func RegisterRoutes(r *gin.Engine, state *common.State) {
	d := r.Group("/derpmap")
	{
		d.GET("", func(c *gin.Context) {
			getDERPMap(c, state)
		})
		d.POST("", func(c *gin.Context) {
			createDERPMap(c, state)
		})
		d.PUT("", func(c *gin.Context) {
			updateDERPMap(c, state)
		})
		d.DELETE("", func(c *gin.Context) {
			deleteDERPMap(c, state)
		})
	}
}

// getDERPMap => GET /derpmap
// Returns the entire ACLDERPMap if it exists, else returns an empty struct or 404.
func getDERPMap(c *gin.Context, state *common.State) {
	dm, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse DERPMap"})
		return
	}
	if dm == nil {
		// If you want a 404:
		// c.JSON(http.StatusNotFound, gin.H{"error": "No DERPMap found"})
		// return
		// Or return an empty one:
		c.JSON(http.StatusOK, tsclient.ACLDERPMap{
			Regions: map[int]*tsclient.ACLDERPRegion{},
		})
		return
	}
	c.JSON(http.StatusOK, dm)
}

// createDERPMap => POST /derpmap
// Creates a new DERPMap if none exists. If one already exists, either overwrite or error.
func createDERPMap(c *gin.Context, state *common.State) {
	var newDM tsclient.ACLDERPMap
	if err := c.ShouldBindJSON(&newDM); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read existing DERPMap"})
		return
	}
	// If you want to error if one already exists:

	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "DERPMap already exists"})
		return
	}

	if err := state.UpdateKeyAndSave("derpMap", newDM); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save DERPMap"})
		return
	}
	c.JSON(http.StatusCreated, newDM)
}

// updateDERPMap => PUT /derpmap
// Updates if exists, or 404 if not found (or create if you prefer).
func updateDERPMap(c *gin.Context, state *common.State) {
	var updated tsclient.ACLDERPMap
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse DERPMap"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No DERPMap found to update"})
		return
	}

	if err := state.UpdateKeyAndSave("derpMap", updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update DERPMap"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteDERPMap => DELETE /derpmap
// Removes DERPMap from state.
func deleteDERPMap(c *gin.Context, state *common.State) {
	existing, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse DERPMap"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No DERPMap found to delete"})
		return
	}

	if err := state.UpdateKeyAndSave("derpMap", nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete DERPMap"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "DERPMap deleted"})
}

// getDERPMapFromState re-marshals state.Data["derpMap"] into *tsclient.ACLDERPMap
func getDERPMapFromState(state *common.State) (*tsclient.ACLDERPMap, error) {
	raw := state.GetValue("derpMap")
	if raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var dm tsclient.ACLDERPMap
	if err := json.Unmarshal(b, &dm); err != nil {
		return nil, err
	}
	return &dm, nil
}
