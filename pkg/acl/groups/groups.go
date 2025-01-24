package groups

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse is used for consistent error response documentation.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Group is the user-facing structure describing a group of members.
// @Description A Group has a required Name and a list of Members.
type Group struct {
	// Name is the unique name of the group (e.g., "engineering").
	Name string `json:"name" binding:"required"`
	// Members is the list of user identifiers or tags belonging to this group.
	Members []string `json:"members"`
}

// DeleteGroupRequest is the shape of the JSON body for deleteGroup.
// Example JSON: { "name": "engineering" }
type DeleteGroupRequest struct {
	Name string `json:"name"`
}

// RegisterRoutes wires up the /groups endpoints.
// The underlying data is stored in state.Data["groups"] as
// a map["group:<Name>"] => []string (members). We convert that map to/from an array of Group.
func RegisterRoutes(r *gin.Engine, state *common.State) {
	g := r.Group("/groups")
	{
		g.GET("", func(c *gin.Context) {
			listGroups(c, state)
		})
		g.GET("/:name", func(c *gin.Context) {
			getGroupByName(c, state)
		})
		g.POST("", func(c *gin.Context) {
			createGroup(c, state)
		})
		g.PUT("", func(c *gin.Context) {
			updateGroup(c, state)
		})
		g.DELETE("", func(c *gin.Context) {
			deleteGroup(c, state)
		})
	}
}

// listGroups => GET /groups => returns entire []Group
// @Summary      List all groups
// @Description  Returns an array of Groups. Each group has a name and members.
// @Tags         Groups
// @Accept       json
// @Produce      json
// @Success      200 {array}  Group
// @Failure      500 {object} ErrorResponse "Failed to parse groups"
// @Router       /groups [get]
func listGroups(c *gin.Context, state *common.State) {
	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse groups"})
		return
	}
	c.JSON(http.StatusOK, groups)
}

// getGroupByName => GET /groups/:name
// @Summary      Get group by name
// @Description  Retrieves a specific group by its name (e.g. "engineering").
// @Tags         Groups
// @Accept       json
// @Produce      json
// @Param        name  path      string  true "Group name"
// @Success      200   {object}  Group
// @Failure      404   {object}  ErrorResponse "Group not found"
// @Failure      500   {object}  ErrorResponse "Failed to parse groups"
// @Router       /groups/{name} [get]
func getGroupByName(c *gin.Context, state *common.State) {
	name := c.Param("name")

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse groups"})
		return
	}

	for _, g := range groups {
		if g.Name == name {
			c.JSON(http.StatusOK, g)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "Group not found"})
}

// createGroup => POST /groups
// @Summary      Create a new group
// @Description  Creates a group with a unique name. Returns 409 if the group name already exists.
// @Tags         Groups
// @Accept       json
// @Produce      json
// @Param        group body Group true "Group to create"
// @Success      201 {object} Group
// @Failure      400 {object} ErrorResponse "Bad request or missing name"
// @Failure      409 {object} ErrorResponse "Group already exists"
// @Failure      500 {object} ErrorResponse "Failed to parse or save groups"
// @Router       /groups [post]
func createGroup(c *gin.Context, state *common.State) {
	var newGroup Group
	if err := c.ShouldBindJSON(&newGroup); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if newGroup.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse groups"})
		return
	}

	// Check if a group with the same name already exists => 409 Conflict
	for _, g := range groups {
		if g.Name == newGroup.Name {
			c.JSON(http.StatusConflict, ErrorResponse{Error: "Group already exists"})
			return
		}
	}

	// Otherwise, append and save
	groups = append(groups, newGroup)
	if err := saveGroups(state, groups); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new group"})
		return
	}
	c.JSON(http.StatusCreated, newGroup)
}

// updateGroup => PUT /groups
// @Summary      Update an existing group
// @Description  Updates the group's members by matching on the group name. Returns 404 if not found.
// @Tags         Groups
// @Accept       json
// @Produce      json
// @Param        group body Group true "Group with updated members"
// @Success      200 {object} Group
// @Failure      400 {object} ErrorResponse "Bad request or missing name"
// @Failure      404 {object} ErrorResponse "Group not found"
// @Failure      500 {object} ErrorResponse "Failed to update group"
// @Router       /groups [put]
func updateGroup(c *gin.Context, state *common.State) {
	var updated Group
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if updated.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse groups"})
		return
	}

	found := false
	for i, g := range groups {
		if g.Name == updated.Name {
			groups[i] = updated
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Group not found"})
		return
	}

	if err := saveGroups(state, groups); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update group"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteGroup => DELETE /groups
// @Summary      Delete a group
// @Description  Deletes a group by name. Expects a JSON body with { "name": "groupName" }.
// @Tags         Groups
// @Accept       json
// @Produce      json
// @Param        body  body      DeleteGroupRequest true "Delete group request"
// @Success      200   {object}  map[string]string   "Group deleted"
// @Failure      400   {object}  ErrorResponse       "Bad request or missing name"
// @Failure      404   {object}  ErrorResponse       "Group not found"
// @Failure      500   {object}  ErrorResponse       "Failed to save changes"
// @Router       /groups [delete]
func deleteGroup(c *gin.Context, state *common.State) {
	var req DeleteGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse groups"})
		return
	}

	found := false
	for i, g := range groups {
		if g.Name == req.Name {
			groups = append(groups[:i], groups[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Group not found"})
		return
	}

	if err := saveGroups(state, groups); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Group deleted"})
}

// -----------------------------------------------------------------------------
// The internal storage format is map["group:<Name>"] => []string (members).
// We convert that map to/from []Group for the user-facing API.
// -----------------------------------------------------------------------------

// getGroupsFromState => read the map => convert to []Group
func getGroupsFromState(state *common.State) ([]Group, error) {
	raw := state.GetValue("groups")
	if raw == nil {
		return []Group{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	// final stored data: map["group:<Name>"] => []string (members)
	var rawMap map[string][]string
	if err := json.Unmarshal(b, &rawMap); err != nil {
		return nil, err
	}

	// Convert map => array
	var out []Group
	for fullKey, members := range rawMap {
		// strip "group:" if present
		name := strings.TrimPrefix(fullKey, "group:")
		out = append(out, Group{
			Name:    name,
			Members: members,
		})
	}
	return out, nil
}

// saveGroups => convert []Group => map => store
func saveGroups(state *common.State, groups []Group) error {
	m := make(map[string][]string)
	for _, g := range groups {
		key := g.Name
		if !strings.HasPrefix(key, "group:") {
			key = "group:" + key
		}
		m[key] = g.Members
	}
	return state.UpdateKeyAndSave("groups", m)
}
