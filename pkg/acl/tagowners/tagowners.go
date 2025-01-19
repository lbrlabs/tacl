package tagowners

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// TagOwner is the user-facing structure. We'll store it in a final map
// at "tagOwners": { "tag:<Name>": [ ...owners... ] }.
type TagOwner struct {
	Name   string   `json:"name" binding:"required"`
	Owners []string `json:"owners"`
}

// RegisterRoutes wires up /tagowners.
func RegisterRoutes(r *gin.Engine, state *common.State) {
	t := r.Group("/tagowners")
	{
		t.GET("", func(c *gin.Context) {
			listTagOwners(c, state)
		})
		t.GET("/:name", func(c *gin.Context) {
			getTagOwnerByName(c, state)
		})
		t.POST("", func(c *gin.Context) {
			createTagOwner(c, state)
		})
		t.PUT("", func(c *gin.Context) {
			updateTagOwner(c, state)
		})
		t.DELETE("", func(c *gin.Context) {
			deleteTagOwner(c, state)
		})
	}
}

// listTagOwners => GET /tagowners
func listTagOwners(c *gin.Context, state *common.State) {
	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse tagOwners"})
		return
	}
	c.JSON(http.StatusOK, tagOwners)
}

// getTagOwnerByName => GET /tagowners/:name
func getTagOwnerByName(c *gin.Context, state *common.State) {
	name := c.Param("name")

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse tagOwners"})
		return
	}

	for _, t := range tagOwners {
		if t.Name == name {
			c.JSON(http.StatusOK, t)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "TagOwner not found"})
}

// createTagOwner => POST /tagowners
// Returns 409 Conflict if name already exists
func createTagOwner(c *gin.Context, state *common.State) {
	var newTag TagOwner
	if err := c.ShouldBindJSON(&newTag); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if newTag.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse tagOwners"})
		return
	}

	// Check for conflict
	for _, t := range tagOwners {
		if t.Name == newTag.Name {
			c.JSON(http.StatusConflict, gin.H{"error": "TagOwner already exists"})
			return
		}
	}

	tagOwners = append(tagOwners, newTag)
	if err := saveTagOwners(state, tagOwners); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new TagOwner"})
		return
	}
	c.JSON(http.StatusCreated, newTag)
}

// updateTagOwner => PUT /tagowners
// Expects JSON: { "name": "...", "owners": [...] }
func updateTagOwner(c *gin.Context, state *common.State) {
	var updated TagOwner
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if updated.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse tagOwners"})
		return
	}

	found := false
	for i, t := range tagOwners {
		if t.Name == updated.Name {
			tagOwners[i] = updated
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "TagOwner not found"})
		return
	}

	if err := saveTagOwners(state, tagOwners); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update TagOwner"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteTagOwner => DELETE /tagowners
// Expects JSON: { "name": "webserver" }
func deleteTagOwner(c *gin.Context, state *common.State) {
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

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse tagOwners"})
		return
	}

	found := false
	for i, t := range tagOwners {
		if t.Name == req.Name {
			tagOwners = append(tagOwners[:i], tagOwners[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "TagOwner not found"})
		return
	}

	if err := saveTagOwners(state, tagOwners); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "TagOwner deleted"})
}

// -----------------------------------------------------------------------------
// Conversions between []TagOwner (API) and map[string][]string (actual storage).
// -----------------------------------------------------------------------------

func getTagOwnersFromState(state *common.State) ([]TagOwner, error) {
	raw := state.GetValue("tagOwners")
	if raw == nil {
		return []TagOwner{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	// final stored data is map["tag:<name>"] => []string
	var rawMap map[string][]string
	if err := json.Unmarshal(b, &rawMap); err != nil {
		return nil, err
	}

	var out []TagOwner
	for fullKey, owners := range rawMap {
		name := strings.TrimPrefix(fullKey, "tag:")
		out = append(out, TagOwner{
			Name:   name,
			Owners: owners,
		})
	}
	return out, nil
}

func saveTagOwners(state *common.State, tagOwners []TagOwner) error {
	m := make(map[string][]string)
	for _, t := range tagOwners {
		fullKey := t.Name
		if !strings.HasPrefix(fullKey, "tag:") {
			fullKey = "tag:" + fullKey
		}
		m[fullKey] = t.Owners
	}
	return state.UpdateKeyAndSave("tagOwners", m)
}
