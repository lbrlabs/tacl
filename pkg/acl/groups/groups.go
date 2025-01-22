package groups

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// Group is the user-facing structure.
type Group struct {
	Name    string   `json:"name" binding:"required"`
	Members []string `json:"members"`
}

// RegisterRoutes wires up the /groups endpoints.
// The final JSON in state.Data["groups"] is map["group:<Name>"] => []string (members).
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

// listGroups => GET /groups
// Returns an array of Groups in memory. The final JSON is a map, so we convert it back to an array here.
func listGroups(c *gin.Context, state *common.State) {
	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse groups"})
		return
	}
	c.JSON(http.StatusOK, groups)
}

// getGroupByName => GET /groups/:name
func getGroupByName(c *gin.Context, state *common.State) {
	name := c.Param("name")

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse groups"})
		return
	}

	for _, g := range groups {
		if g.Name == name {
			c.JSON(http.StatusOK, g)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
}

// createGroup => POST /groups
// 409 Conflict if the name already exists
func createGroup(c *gin.Context, state *common.State) {
	var newGroup Group
	if err := c.ShouldBindJSON(&newGroup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if newGroup.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse groups"})
		return
	}

	// Check if a group with the same name already exists => 409 Conflict
	for _, g := range groups {
		if g.Name == newGroup.Name {
			c.JSON(http.StatusConflict, gin.H{"error": "Group already exists"})
			return
		}
	}

	// Otherwise, append and save
	groups = append(groups, newGroup)
	if err := saveGroups(state, groups); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new group"})
		return
	}
	c.JSON(http.StatusCreated, newGroup)
}

// updateGroup => PUT /groups
// Expects { "name":"engineering", "members":[]... }
func updateGroup(c *gin.Context, state *common.State) {
	var updated Group
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if updated.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse groups"})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	if err := saveGroups(state, groups); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update group"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteGroup => DELETE /groups
// Expects JSON: { "name": "engineering" }
func deleteGroup(c *gin.Context, state *common.State) {
	type request struct {
		Name string `json:"name"`
	}
	var req request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	groups, err := getGroupsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse groups"})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Group not found"})
		return
	}

	if err := saveGroups(state, groups); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Group deleted"})
}

// -----------------------------------------------------------------------------
// Conversion: We want final JSON => "groups": { "group:<Name>": [...members...] }
// But user sees array of Group {Name, Members}.
//
// getGroupsFromState => read map => convert to []Group
// saveGroups => convert []Group => map => store
// -----------------------------------------------------------------------------

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
