package nodeattrs

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// RegisterRoutes wires up CRUD endpoints at /nodeattrs.
//
// For example:
//
//	GET    /nodeattrs         => list all NodeAttrGrant
//	GET    /nodeattrs/:index  => get one by index
//	POST   /nodeattrs         => create new
//	PUT    /nodeattrs         => update existing by { "index": N, "grant": {...} }
//	DELETE /nodeattrs         => remove by { "index": N }
func RegisterRoutes(r *gin.Engine, state *common.State) {
	n := r.Group("/nodeattrs")
	{
		n.GET("", func(c *gin.Context) {
			listNodeAttrs(c, state)
		})
		n.GET("/:index", func(c *gin.Context) {
			getNodeAttrByIndex(c, state)
		})
		n.POST("", func(c *gin.Context) {
			createNodeAttr(c, state)
		})
		n.PUT("", func(c *gin.Context) {
			updateNodeAttr(c, state)
		})
		n.DELETE("", func(c *gin.Context) {
			deleteNodeAttr(c, state)
		})
	}
}

// listNodeAttrs => GET /nodeattrs
// Returns all NodeAttrGrant objects in state.Data["nodeAttrs"].
func listNodeAttrs(c *gin.Context, state *common.State) {
	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}
	c.JSON(http.StatusOK, grants)
}

// getNodeAttrByIndex => GET /nodeattrs/:index
func getNodeAttrByIndex(c *gin.Context, state *common.State) {
	indexStr := c.Param("index")
	i, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid index"})
		return
	}

	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}

	if i < 0 || i >= len(grants) {
		c.JSON(http.StatusNotFound, gin.H{"error": "NodeAttr index out of range"})
		return
	}
	c.JSON(http.StatusOK, grants[i])
}

// createNodeAttr => POST /nodeattrs
// Appends a new NodeAttrGrant to the existing slice.
func createNodeAttr(c *gin.Context, state *common.State) {
	var newGrant tsclient.NodeAttrGrant
	if err := c.ShouldBindJSON(&newGrant); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}

	grants = append(grants, newGrant)
	if err := state.UpdateKeyAndSave("nodeAttrs", grants); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save node attribute"})
		return
	}
	c.JSON(http.StatusCreated, newGrant)
}

// updateNodeAttr => PUT /nodeattrs
// Expects JSON with an 'index' and a 'grant' object.
func updateNodeAttr(c *gin.Context, state *common.State) {
	type updateRequest struct {
		Index int                    `json:"index"`
		Grant tsclient.NodeAttrGrant `json:"grant"`
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Index < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid 'index' field"})
		return
	}

	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}

	if req.Index >= len(grants) {
		c.JSON(http.StatusNotFound, gin.H{"error": "NodeAttr index out of range"})
		return
	}

	grants[req.Index] = req.Grant
	if err := state.UpdateKeyAndSave("nodeAttrs", grants); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update node attribute"})
		return
	}
	c.JSON(http.StatusOK, req.Grant)
}

// deleteNodeAttr => DELETE /nodeattrs
// Expects JSON with an 'index'.
func deleteNodeAttr(c *gin.Context, state *common.State) {
	type deleteRequest struct {
		Index int `json:"index"`
	}
	var req deleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}

	if req.Index < 0 || req.Index >= len(grants) {
		c.JSON(http.StatusNotFound, gin.H{"error": "NodeAttr index out of range"})
		return
	}

	grants = append(grants[:req.Index], grants[req.Index+1:]...)
	if err := state.UpdateKeyAndSave("nodeAttrs", grants); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete node attribute"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Node attribute deleted"})
}

// getNodeAttrsFromState => re-marshal state.Data["nodeAttrs"] -> []tsclient.NodeAttrGrant
func getNodeAttrsFromState(state *common.State) ([]tsclient.NodeAttrGrant, error) {
	raw := state.GetValue("nodeAttrs") // uses RLock internally
	if raw == nil {
		return []tsclient.NodeAttrGrant{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var grants []tsclient.NodeAttrGrant
	if err := json.Unmarshal(b, &grants); err != nil {
		return nil, err
	}
	return grants, nil
}
