// pkg/acl/nodeattributes/nodeattributes.go

package nodeattrs

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// NodeAttrGrantInput => incoming JSON in POST/PUT for the "grant" object.
type NodeAttrGrantInput struct {
	Target []string                       `json:"target" binding:"required"`
	Attr   []string                       `json:"attr,omitempty"`
	App    map[string][]AppConnectorInput `json:"app,omitempty"`
}

// AppConnectorInput => each item in app
type AppConnectorInput struct {
	Name       string   `json:"name,omitempty"`
	Connectors []string `json:"connectors,omitempty"`
	Domains    []string `json:"domains,omitempty"`
}

// ExtendedNodeAttrGrant => we store in TACL. target => required, attr OR app => exactly one
type ExtendedNodeAttrGrant struct {
	tsclient.NodeAttrGrant
	App map[string][]AppConnectorInput `json:"app,omitempty"`
}

// RegisterRoutes => /nodeattrs
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

// listNodeAttrs => GET /nodeattrs => returns array of ExtendedNodeAttrGrant
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
// Expects NodeAttrGrantInput => either "attr" or "app" must be set, not both
// createNodeAttr => POST /nodeattrs
func createNodeAttr(c *gin.Context, state *common.State) {
    var input NodeAttrGrantInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // check exclusivity
    if !exactlyOneOfAttrOrApp(input) {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Either `attr` or `app` must be set, but not both"})
        return
    }

    // If we're dealing with `app`, force target = ["*"]
    if len(input.App) > 0 {
        input.Target = []string{"*"}
    }

    grants, err := getNodeAttrsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
        return
    }

    newGrant := ExtendedNodeAttrGrant{
        NodeAttrGrant: tsclient.NodeAttrGrant{
            Target: input.Target,
            Attr:   input.Attr,
        },
        App: convertAppConnectors(input.App),
    }

    grants = append(grants, newGrant)
    if err := state.UpdateKeyAndSave("nodeAttrs", grants); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save node attribute"})
        return
    }
    c.JSON(http.StatusCreated, newGrant)
}

// updateNodeAttr => PUT /nodeattrs => { index, grant: NodeAttrGrantInput }
// updateNodeAttr => PUT /nodeattrs => { index, grant: NodeAttrGrantInput }
func updateNodeAttr(c *gin.Context, state *common.State) {
    type updateRequest struct {
        Index int                `json:"index"`
        Grant NodeAttrGrantInput `json:"grant"`
    }
    var req updateRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    if req.Index < 0 {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid index"})
        return
    }

    if !exactlyOneOfAttrOrApp(req.Grant) {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Either `attr` or `app` must be set, but not both"})
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

    // If `app` is present, force target = ["*"]
    if len(req.Grant.App) > 0 {
        req.Grant.Target = []string{"*"}
    }

    updated := ExtendedNodeAttrGrant{
        NodeAttrGrant: tsclient.NodeAttrGrant{
            Target: req.Grant.Target,
            Attr:   req.Grant.Attr,
        },
        App: convertAppConnectors(req.Grant.App),
    }

    grants[req.Index] = updated
    if err := state.UpdateKeyAndSave("nodeAttrs", grants); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update node attribute"})
        return
    }
    c.JSON(http.StatusOK, updated)
}


// deleteNodeAttr => DELETE /nodeattrs => { index }
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

// getNodeAttrsFromState => read state.Data["nodeAttrs"]
func getNodeAttrsFromState(state *common.State) ([]ExtendedNodeAttrGrant, error) {
	raw := state.GetValue("nodeAttrs")
	if raw == nil {
		return []ExtendedNodeAttrGrant{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var grants []ExtendedNodeAttrGrant
	if err := json.Unmarshal(b, &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

func convertAppConnectors(in map[string][]AppConnectorInput) map[string][]AppConnectorInput {
	if in == nil {
		return nil
	}
	out := make(map[string][]AppConnectorInput, len(in))
	for key, arr := range in {
		list := make([]AppConnectorInput, len(arr))
		copy(list, arr) // or expand if needed
		out[key] = list
	}
	return out
}

func exactlyOneOfAttrOrApp(input NodeAttrGrantInput) bool {
	hasAttr := len(input.Attr) > 0
	hasApp  := len(input.App) > 0
	// true only if one is true and the other is false
	return (hasAttr || hasApp) && !(hasAttr && hasApp)
}
