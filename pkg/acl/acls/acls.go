// pkg/acl/acls/acls.go
package acls

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse can be used in @Failure annotations so we get a more descriptive schema than map[string]string.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ACL represents the fields of an ACL rule.
// @Description ACL defines the action, source, destination, and optional posture for a single rule.
type ACL struct {
	// Action specifies the rule action (e.g. "accept" or "deny").
	Action string `json:"action,omitempty" hujson:"Action,omitempty"`

	// Source is a list of CIDRs or tags that match the traffic source.
	Source []string `json:"src,omitempty" hujson:"Src,omitempty"`

	// Destination is a list of CIDRs or tags that match the traffic destination.
	Destination []string `json:"dst,omitempty" hujson:"Dst,omitempty"`

	// Protocol (proto) can specify "tcp", "udp", etc.
	Protocol string `json:"proto,omitempty" hujson:"Proto,omitempty"`

	// SourcePosture is for an experimental feature and not yet public or documented as of 2023-08-17.
	SourcePosture []string `json:"srcPosture,omitempty" hujson:"SrcPosture,omitempty"`
}

// ExtendedACLEntry is a local storage type with a stable UUID plus ACL fields.
// @Description ExtendedACLEntry wraps an ACL with a unique ID for local storage.
type ExtendedACLEntry struct {
	ID string `json:"id"` // stable UUID

	ACL
}

// updateRequest represents the body shape for PUT /acls.
//
// Example JSON:
//
//	{
//	  "id": "some-uuid",
//	  "entry": {
//	    "action": "accept",
//	    "src": ["10.0.0.0/24"],
//	    "dst": ["10.0.1.0/24"]
//	  }
//	}
type updateRequest struct {
	ID    string `json:"id"`
	Entry ACL    `json:"entry"`
}

// deleteRequest represents the body shape for DELETE /acls.
//
// Example JSON:
//
//	{ "id": "some-uuid" }
type deleteRequest struct {
	ID string `json:"id"`
}

// RegisterRoutes wires up ACL-related routes at /acls:
//
//   GET    /acls         => list all (by ID)
//   GET    /acls/:id     => get one by ID
//   POST   /acls         => create (generate a new ID)
//   PUT    /acls         => update an existing ACL by ID
//   DELETE /acls         => delete by ID
func RegisterRoutes(r *gin.Engine, state *common.State) {
	a := r.Group("/acls")
	{
		a.GET("", func(c *gin.Context) {
			listACLs(c, state)
		})

		a.GET("/:id", func(c *gin.Context) {
			getACLByID(c, state)
		})

		a.POST("", func(c *gin.Context) {
			createACL(c, state)
		})

		a.PUT("", func(c *gin.Context) {
			updateACL(c, state)
		})

		a.DELETE("", func(c *gin.Context) {
			deleteACL(c, state)
		})
	}
}

// listACLs => GET /acls => returns entire []ExtendedACLEntry
// @Summary      List all ACL entries
// @Description  Returns the entire list of ExtendedACLEntry objects.
// @Tags         ACLs
// @Accept       json
// @Produce      json
// @Success      200 {array}  ExtendedACLEntry "List of ACL entries"
// @Failure      500 {object} ErrorResponse "Failed to parse ACLs"
// @Router       /acls [get]
func listACLs(c *gin.Context, state *common.State) {
	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLs"})
		return
	}
	c.JSON(http.StatusOK, acls)
}

// getACLByID => GET /acls/:id
// @Summary      Get one ACL by ID
// @Description  Retrieves a single ACL entry by its stable UUID.
// @Tags         ACLs
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "ACL ID"
// @Success      200  {object}  ExtendedACLEntry
// @Failure      404  {object}  ErrorResponse "ACL entry not found"
// @Failure      500  {object}  ErrorResponse "Failed to parse ACLs"
// @Router       /acls/{id} [get]
func getACLByID(c *gin.Context, state *common.State) {
	id := c.Param("id")

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLs"})
		return
	}

	for _, entry := range acls {
		if entry.ID == id {
			c.JSON(http.StatusOK, entry)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "ACL entry not found"})
}

// createACL => POST /acls
// @Summary      Create a new ACL
// @Description  Creates a new ACL by generating a new UUID and storing the provided ACL fields.
// @Tags         ACLs
// @Accept       json
// @Produce      json
// @Param        acl  body      ACL  true  "ACL fields"
// @Success      201  {object}  ExtendedACLEntry
// @Failure      400  {object}  ErrorResponse "Bad request"
// @Failure      500  {object}  ErrorResponse "Failed to save new ACL entry"
// @Router       /acls [post]
func createACL(c *gin.Context, state *common.State) {
	var newData ACL
	if err := c.ShouldBindJSON(&newData); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLs"})
		return
	}

	newEntry := ExtendedACLEntry{
		ID:  uuid.NewString(),
		ACL: newData,
	}

	acls = append(acls, newEntry)
	if err := state.UpdateKeyAndSave("acls", acls); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new ACL entry"})
		return
	}
	c.JSON(http.StatusCreated, newEntry)
}

// updateACL => PUT /acls
// @Summary      Update an existing ACL
// @Description  Updates the ACL fields for an entry identified by its UUID.
// @Tags         ACLs
// @Accept       json
// @Produce      json
// @Param        body  body      updateRequest true "Update ACL request"
// @Success      200   {object}  ExtendedACLEntry
// @Failure      400   {object}  ErrorResponse "Missing or invalid request data"
// @Failure      404   {object}  ErrorResponse "ACL entry not found"
// @Failure      500   {object}  ErrorResponse "Failed to update ACL entry"
// @Router       /acls [put]
func updateACL(c *gin.Context, state *common.State) {
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing ACL 'id' in request body"})
		return
	}

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLs"})
		return
	}

	var updated *ExtendedACLEntry
	for i := range acls {
		if acls[i].ID == req.ID {
			// Found => update the embedded ACL
			acls[i].ACL = req.Entry
			updated = &acls[i]
			break
		}
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "ACL entry not found"})
		return
	}

	if err := state.UpdateKeyAndSave("acls", acls); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update ACL entry"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteACL => DELETE /acls => body => { "id": "<uuid>" }
// @Summary      Delete an ACL
// @Description  Deletes an ACL entry by specifying its ID in the request body.
// @Tags         ACLs
// @Accept       json
// @Produce      json
// @Param        body  body      deleteRequest true "Delete ACL request"
// @Success      200   {object}  map[string]string "ACL entry deleted"
// @Failure      400   {object}  ErrorResponse "Missing or invalid ID"
// @Failure      404   {object}  ErrorResponse "ACL entry not found with that ID"
// @Failure      500   {object}  ErrorResponse "Failed to delete ACL entry"
// @Router       /acls [delete]
func deleteACL(c *gin.Context, state *common.State) {
	var req deleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'id' field"})
		return
	}

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLs"})
		return
	}

	newList := make([]ExtendedACLEntry, 0, len(acls))
	deleted := false
	for _, entry := range acls {
		if entry.ID == req.ID {
			deleted = true
			continue // skip => effectively remove
		}
		newList = append(newList, entry)
	}
	if !deleted {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "ACL entry not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("acls", newList); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete ACL entry"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ACL entry deleted"})
}

// getACLsFromState => read state.Data["acls"] => []ExtendedACLEntry
func getACLsFromState(state *common.State) ([]ExtendedACLEntry, error) {
	raw := state.GetValue("acls")
	if raw == nil {
		return []ExtendedACLEntry{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var acls []ExtendedACLEntry
	if err := json.Unmarshal(b, &acls); err != nil {
		return nil, err
	}
	return acls, nil
}
