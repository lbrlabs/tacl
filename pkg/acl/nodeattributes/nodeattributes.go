// pkg/acl/nodeattributes/nodeattributes.go
package nodeattrs

import (
    "encoding/json"
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "github.com/lbrlabs/tacl/pkg/common"
    tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// NodeAttrGrantInput => incoming JSON for create/update
// Exactly one of Attr or App must be set
type NodeAttrGrantInput struct {
    Target []string                       `json:"target" binding:"required"`
    Attr   []string                       `json:"attr,omitempty"`
    App    map[string][]AppConnectorInput `json:"app,omitempty"`
}

// AppConnectorInput => each item in "app"
type AppConnectorInput struct {
    Name       string   `json:"name,omitempty"`
    Connectors []string `json:"connectors,omitempty"`
    Domains    []string `json:"domains,omitempty"`
}

// ExtendedNodeAttrGrant => local storage in TACL, including a stable "id" for Terraform.
type ExtendedNodeAttrGrant struct {
    ID string `json:"id"` // Local stable ID (UUID)

    tsclient.NodeAttrGrant
    App map[string][]AppConnectorInput `json:"app,omitempty"`
}

// RegisterRoutes => sets up /nodeattrs endpoints
func RegisterRoutes(r *gin.Engine, state *common.State) {
    n := r.Group("/nodeattrs")
    {
        // List all
        n.GET("", func(c *gin.Context) {
            listNodeAttrs(c, state)
        })
        // Get one by ID
        n.GET("/:id", func(c *gin.Context) {
            getNodeAttrByID(c, state)
        })
        // Create
        n.POST("", func(c *gin.Context) {
            createNodeAttr(c, state)
        })
        // Update by sending { "id": "<uuid>", "grant": { ... } }
        n.PUT("", func(c *gin.Context) {
            updateNodeAttr(c, state)
        })
        // Delete by sending { "id": "<uuid>" }
        n.DELETE("", func(c *gin.Context) {
            deleteNodeAttr(c, state)
        })
    }
}

// listNodeAttrs => GET /nodeattrs => returns all ExtendedNodeAttrGrant
func listNodeAttrs(c *gin.Context, state *common.State) {
    grants, err := getNodeAttrsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
        return
    }
    c.JSON(http.StatusOK, grants)
}

// getNodeAttrByID => GET /nodeattrs/:id
func getNodeAttrByID(c *gin.Context, state *common.State) {
    id := c.Param("id")

    grants, err := getNodeAttrsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
        return
    }

    for _, g := range grants {
        if g.ID == id {
            c.JSON(http.StatusOK, g)
            return
        }
    }
    c.JSON(http.StatusNotFound, gin.H{"error": "No nodeattr found with that id"})
}

// createNodeAttr => POST /nodeattrs (body => NodeAttrGrantInput)
func createNodeAttr(c *gin.Context, state *common.State) {
    var input NodeAttrGrantInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // exactly one of attr or app
    if !exactlyOneOfAttrOrApp(input) {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Either `attr` or `app` must be set, but not both"})
        return
    }

    // If using app, force target=["*"]
    if len(input.App) > 0 {
        input.Target = []string{"*"}
    }

    grants, err := getNodeAttrsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
        return
    }

    newGrant := ExtendedNodeAttrGrant{
        ID: uuid.NewString(),
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

// updateNodeAttr => PUT /nodeattrs => body shape:
// {
//   "id": "<uuid>",
//   "grant": { "target": [...], "attr": [...], "app": {...} }
// }
func updateNodeAttr(c *gin.Context, state *common.State) {
    type updateRequest struct {
        ID    string           `json:"id"`
        Grant NodeAttrGrantInput `json:"grant"`
    }
    var req updateRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    if req.ID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'id' in request body"})
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

    // If app present => force target=["*"]
    if len(req.Grant.App) > 0 {
        req.Grant.Target = []string{"*"}
    }

    var updated *ExtendedNodeAttrGrant
    for i := range grants {
        if grants[i].ID == req.ID {
            grants[i].Target = req.Grant.Target
            grants[i].Attr = req.Grant.Attr
            grants[i].App = convertAppConnectors(req.Grant.App)
            updated = &grants[i]
            break
        }
    }
    if updated == nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "NodeAttr not found with that id"})
        return
    }

    if err := state.UpdateKeyAndSave("nodeAttrs", grants); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update node attribute"})
        return
    }
    c.JSON(http.StatusOK, updated)
}

// deleteNodeAttr => DELETE /nodeattrs => { "id": "<uuid>" }
func deleteNodeAttr(c *gin.Context, state *common.State) {
    type deleteRequest struct {
        ID string `json:"id"`
    }
    var req deleteRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    if req.ID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'id' field"})
        return
    }

    grants, err := getNodeAttrsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
        return
    }

    newList := make([]ExtendedNodeAttrGrant, 0, len(grants))
    deleted := false
    for _, g := range grants {
        if g.ID == req.ID {
            // skip it
            deleted = true
            continue
        }
        newList = append(newList, g)
    }
    if !deleted {
        c.JSON(http.StatusNotFound, gin.H{"error": "NodeAttr not found with that id"})
        return
    }

    if err := state.UpdateKeyAndSave("nodeAttrs", newList); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete node attribute"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "Node attribute deleted"})
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

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
        copy(list, arr)
        out[key] = list
    }
    return out
}

func exactlyOneOfAttrOrApp(input NodeAttrGrantInput) bool {
    hasAttr := len(input.Attr) > 0
    hasApp := len(input.App) > 0
    return (hasAttr || hasApp) && !(hasAttr && hasApp)
}
