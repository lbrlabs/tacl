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

// -----------------------------------------------------------------------------
// 1) Doc-Only Types for Swag
// -----------------------------------------------------------------------------

// ErrorResponse is used for error responses in @Failure annotations.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AppConnectorInputDoc duplicates AppConnectorInput for Swag docs.
// It's used under "app" in NodeAttrGrantInputDoc or ExtendedNodeAttrGrantDoc.
type AppConnectorInputDoc struct {
	Name       string   `json:"name,omitempty"`
	Connectors []string `json:"connectors,omitempty"`
	Domains    []string `json:"domains,omitempty"`
}

// NodeAttrGrantInputDoc duplicates NodeAttrGrantInput for doc.
// "Either `attr` or `app` must be set, but not both."
type NodeAttrGrantInputDoc struct {
	// Target is a list of node targets (could be ["*"] if using app).
	Target []string `json:"target" binding:"required"`
	// Attr is a list of attribute strings if not using "app".
	Attr []string `json:"attr,omitempty"`
	// App is a map of <string> to []AppConnectorInputDoc if not using "attr".
	App map[string][]AppConnectorInputDoc `json:"app,omitempty"`
}

// ExtendedNodeAttrGrantDoc is the doc version of ExtendedNodeAttrGrant,
// including a stable "id" and the fields from NodeAttrGrant plus "app".
type ExtendedNodeAttrGrantDoc struct {
	// ID is the local stable UUID.
	ID string `json:"id"`
	// Target is the list of node targets for the attribute grant.
	Target []string `json:"target"`
	// Attr is the list of attributes if this is an attr-based grant.
	Attr []string `json:"attr,omitempty"`
	// App is present if this is an app-based grant.
	App map[string][]AppConnectorInputDoc `json:"app,omitempty"`
}

// updateNodeAttrRequestDoc duplicates the PUT request body:
//
//	{
//	  "id": "<uuid>",
//	  "grant": { "target": [...], "attr": [...], "app": {...} }
//	}
type updateNodeAttrRequestDoc struct {
	ID    string               `json:"id"`
	Grant NodeAttrGrantInputDoc `json:"grant"`
}

// deleteNodeAttrRequestDoc is the shape for DELETE /nodeattrs.
//
//	{
//	  "id": "<uuid>"
//	}
type deleteNodeAttrRequestDoc struct {
	ID string `json:"id"`
}

// -----------------------------------------------------------------------------
// 2) Actual Runtime Types (unchanged) + Route Registration
// -----------------------------------------------------------------------------

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
//
//   GET    /nodeattrs        => list all ExtendedNodeAttrGrant
//   GET    /nodeattrs/:id    => get one by ID
//   POST   /nodeattrs        => create new nodeattr
//   PUT    /nodeattrs        => update existing by ID
//   DELETE /nodeattrs        => delete by ID
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
		// Update
		n.PUT("", func(c *gin.Context) {
			updateNodeAttr(c, state)
		})
		// Delete
		n.DELETE("", func(c *gin.Context) {
			deleteNodeAttr(c, state)
		})
	}
}

// -----------------------------------------------------------------------------
// 3) Endpoints (Swag Annotations referencing doc-only structs)
// -----------------------------------------------------------------------------

// listNodeAttrs => GET /nodeattrs => returns all ExtendedNodeAttrGrant
// @Summary      List all node attribute grants
// @Description  Returns the entire list of ExtendedNodeAttrGrant objects from state.
// @Tags         NodeAttrs
// @Accept       json
// @Produce      json
// @Success      200 {array}  ExtendedNodeAttrGrantDoc
// @Failure      500 {object} ErrorResponse "Failed to parse node attributes"
// @Router       /nodeattrs [get]
func listNodeAttrs(c *gin.Context, state *common.State) {
	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}

	// Convert actual ExtendedNodeAttrGrant to doc structs
	docs := make([]ExtendedNodeAttrGrantDoc, 0, len(grants))
	for _, realGrant := range grants {
		docs = append(docs, convertRealGrantToDoc(realGrant))
	}
	c.JSON(http.StatusOK, docs)
}

// getNodeAttrByID => GET /nodeattrs/:id
// @Summary      Get node attribute grant by ID
// @Description  Retrieves a single ExtendedNodeAttrGrant by its stable UUID.
// @Tags         NodeAttrs
// @Accept       json
// @Produce      json
// @Param        id  path string true "NodeAttrGrant ID"
// @Success      200 {object} ExtendedNodeAttrGrantDoc
// @Failure      404 {object} ErrorResponse "No nodeattr found with that id"
// @Failure      500 {object} ErrorResponse "Failed to parse node attributes"
// @Router       /nodeattrs/{id} [get]
func getNodeAttrByID(c *gin.Context, state *common.State) {
	id := c.Param("id")

	grants, err := getNodeAttrsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse node attributes"})
		return
	}

	for _, g := range grants {
		if g.ID == id {
			c.JSON(http.StatusOK, convertRealGrantToDoc(g))
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "No nodeattr found with that id"})
}

// createNodeAttr => POST /nodeattrs
// @Summary      Create a new node attribute grant
// @Description  Creates a new ExtendedNodeAttrGrant with either `attr` or `app`. If `app` is set, `target` is forced to ["*"].
// @Tags         NodeAttrs
// @Accept       json
// @Produce      json
// @Param        grant body NodeAttrGrantInputDoc true "NodeAttrGrant input"
// @Success      201 {object} ExtendedNodeAttrGrantDoc
// @Failure      400 {object} ErrorResponse "Either 'attr' or 'app' must be set, but not both"
// @Failure      500 {object} ErrorResponse "Failed to parse node attributes or save new grant"
// @Router       /nodeattrs [post]
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
	c.JSON(http.StatusCreated, convertRealGrantToDoc(newGrant))
}

// updateNodeAttr => PUT /nodeattrs
// @Summary      Update an existing node attribute grant
// @Description  Updates a grant by ID. If `app` is set, `target` is forced to ["*"].
// @Tags         NodeAttrs
// @Accept       json
// @Produce      json
// @Param        body body updateNodeAttrRequestDoc true "Update NodeAttr request"
// @Success      200 {object} ExtendedNodeAttrGrantDoc
// @Failure      400 {object} ErrorResponse "Invalid JSON or missing fields"
// @Failure      404 {object} ErrorResponse "NodeAttr not found"
// @Failure      500 {object} ErrorResponse "Failed to parse or update node attribute"
// @Router       /nodeattrs [put]
func updateNodeAttr(c *gin.Context, state *common.State) {
	type updateRequest struct {
		ID    string             `json:"id"`
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
	c.JSON(http.StatusOK, convertRealGrantToDoc(*updated))
}

// deleteNodeAttr => DELETE /nodeattrs
// @Summary      Delete a node attribute grant
// @Description  Deletes by specifying its ID in the request body.
// @Tags         NodeAttrs
// @Accept       json
// @Produce      json
// @Param        body body deleteNodeAttrRequestDoc true "Delete NodeAttr request"
// @Success      200 {object} map[string]string "Node attribute deleted"
// @Failure      400 {object} ErrorResponse "Missing or invalid ID"
// @Failure      404 {object} ErrorResponse "NodeAttr not found with that id"
// @Failure      500 {object} ErrorResponse "Failed to delete node attribute"
// @Router       /nodeattrs [delete]
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
// 4) Helper / Conversion Functions
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

// convertRealGrantToDoc transforms a runtime ExtendedNodeAttrGrant to the doc version ExtendedNodeAttrGrantDoc.
func convertRealGrantToDoc(real ExtendedNodeAttrGrant) ExtendedNodeAttrGrantDoc {
	// Convert each app connector to doc form
	docApp := make(map[string][]AppConnectorInputDoc, len(real.App))
	for key, arr := range real.App {
		docs := make([]AppConnectorInputDoc, len(arr))
		for i, item := range arr {
			docs[i] = AppConnectorInputDoc{
				Name:       item.Name,
				Connectors: item.Connectors,
				Domains:    item.Domains,
			}
		}
		docApp[key] = docs
	}

	return ExtendedNodeAttrGrantDoc{
		ID:     real.ID,
		Target: real.Target,
		Attr:   real.Attr,
		App:    docApp,
	}
}

// exactlyOneOfAttrOrApp checks that NodeAttrGrantInput has either .Attr or .App, but not both.
func exactlyOneOfAttrOrApp(input NodeAttrGrantInput) bool {
	hasAttr := len(input.Attr) > 0
	hasApp := len(input.App) > 0
	return (hasAttr || hasApp) && !(hasAttr && hasApp)
}
