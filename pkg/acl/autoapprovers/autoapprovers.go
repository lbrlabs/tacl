package autoapprovers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// ErrorResponse is used in @Failure annotations for descriptive error messages.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ACLAutoApproversDoc duplicates fields from tsclient.ACLAutoApprovers
// so we can reference it in Swagger without parse errors.
//
// routes:   map[string][]string
// exitNode: []string
type ACLAutoApproversDoc struct {
	Routes   map[string][]string `json:"routes,omitempty"`
	ExitNode []string            `json:"exitNode,omitempty"`
}

// RegisterRoutes wires up auto-approver routes under /autoapprovers.
//
//   GET    /autoapprovers => retrieve the entire ACLAutoApprovers struct
//   POST   /autoapprovers => create or set it (if none exists)
//   PUT    /autoapprovers => update it (if one exists)
//   DELETE /autoapprovers => remove it from the state
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
// @Summary      Get auto-approvers
// @Description  Returns the entire auto-approvers struct if present, otherwise returns an empty object or 404.
// @Tags         AutoApprovers
// @Accept       json
// @Produce      json
// @Success      200 {object} ACLAutoApproversDoc "Auto-approvers found (or empty)"
// @Failure      500 {object} ErrorResponse       "Failed to parse autoApprovers"
// @Router       /autoapprovers [get]
func getAutoApprovers(c *gin.Context, state *common.State) {
	aap, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse autoApprovers"})
		return
	}
	if aap == nil {
		// Return an empty doc. If you prefer 404, do: c.JSON(http.StatusNotFound, ...)
		c.JSON(http.StatusOK, ACLAutoApproversDoc{
			Routes:   map[string][]string{},
			ExitNode: []string{},
		})
		return
	}
	c.JSON(http.StatusOK, convertToDoc(*aap))
}

// createAutoApprovers => POST /autoapprovers
// @Summary      Create auto-approvers
// @Description  Creates a new ACLAutoApprovers if none exists. Returns 409 if one already exists.
// @Tags         AutoApprovers
// @Accept       json
// @Produce      json
// @Param        autoApprovers body ACLAutoApproversDoc true "AutoApprovers data"
// @Success      201 {object} ACLAutoApproversDoc
// @Failure      400 {object} ErrorResponse "Invalid JSON body"
// @Failure      409 {object} ErrorResponse "autoApprovers already exists"
// @Failure      500 {object} ErrorResponse "Failed to parse or save autoApprovers"
// @Router       /autoapprovers [post]
func createAutoApprovers(c *gin.Context, state *common.State) {
	var newAAPDoc ACLAutoApproversDoc
	if err := c.ShouldBindJSON(&newAAPDoc); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	existing, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to check existing autoApprovers"})
		return
	}

	if existing != nil {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "autoApprovers already exists"})
		return
	}

	newAAP := convertFromDoc(newAAPDoc)
	if err := state.UpdateKeyAndSave("autoApprovers", newAAP); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save autoApprovers"})
		return
	}
	c.JSON(http.StatusCreated, newAAPDoc)
}

// updateAutoApprovers => PUT /autoapprovers
// @Summary      Update auto-approvers
// @Description  Updates an existing auto-approvers struct. If none exists, returns 404.
// @Tags         AutoApprovers
// @Accept       json
// @Produce      json
// @Param        autoApprovers body ACLAutoApproversDoc true "Updated autoApprovers data"
// @Success      200 {object} ACLAutoApproversDoc
// @Failure      400 {object} ErrorResponse "Invalid JSON body"
// @Failure      404 {object} ErrorResponse "No autoApprovers found to update"
// @Failure      500 {object} ErrorResponse "Failed to update autoApprovers"
// @Router       /autoapprovers [put]
func updateAutoApprovers(c *gin.Context, state *common.State) {
	var updatedDoc ACLAutoApproversDoc
	if err := c.ShouldBindJSON(&updatedDoc); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	existing, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse autoApprovers"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No autoApprovers found to update"})
		return
	}

	newAAP := convertFromDoc(updatedDoc)
	if err := state.UpdateKeyAndSave("autoApprovers", newAAP); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update autoApprovers"})
		return
	}
	c.JSON(http.StatusOK, updatedDoc)
}

// deleteAutoApprovers => DELETE /autoapprovers
// @Summary      Delete auto-approvers
// @Description  Removes the autoApprovers from state. If none exists, returns 404.
// @Tags         AutoApprovers
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]string "autoApprovers deleted"
// @Failure      404 {object} ErrorResponse "No autoApprovers found"
// @Failure      500 {object} ErrorResponse "Failed to delete autoApprovers"
// @Router       /autoapprovers [delete]
func deleteAutoApprovers(c *gin.Context, state *common.State) {
	existing, err := getAutoApproversFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse autoApprovers"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No autoApprovers found"})
		return
	}

	if err := state.UpdateKeyAndSave("autoApprovers", nil); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete autoApprovers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "autoApprovers deleted"})
}

// getAutoApproversFromState re-marshal state.Data["autoApprovers"] to *tsclient.ACLAutoApprovers
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

// convertToDoc transforms the real tsclient.ACLAutoApprovers into ACLAutoApproversDoc.
func convertToDoc(aap tsclient.ACLAutoApprovers) ACLAutoApproversDoc {
	return ACLAutoApproversDoc{
		Routes:   aap.Routes,
		ExitNode: aap.ExitNode,
	}
}

// convertFromDoc transforms ACLAutoApproversDoc into the real tsclient.ACLAutoApprovers.
func convertFromDoc(doc ACLAutoApproversDoc) tsclient.ACLAutoApprovers {
	return tsclient.ACLAutoApprovers{
		Routes:   doc.Routes,
		ExitNode: doc.ExitNode,
	}
}
