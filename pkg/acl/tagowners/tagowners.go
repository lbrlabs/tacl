package tagowners

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse helps standardize error output in Swagger.
type ErrorResponse struct {
	Error string `json:"error"`
}

// TagOwner is the user-facing structure.
//   "tagOwners": { "tag:<Name>": [ ...owners... ] }.
//
// @Description TagOwner associates a tag name (e.g. "webserver") with a list of owners.
type TagOwner struct {
	// Name is the name of the tag (e.g. "webserver").
	Name string `json:"name" binding:"required"`
	// Owners is a list of owners for this tag.
	Owners []string `json:"owners"`
}

// deleteTagOwnerRequest is the body shape for DELETE /tagowners.
//
// Example: { "name": "webserver" }
type deleteTagOwnerRequest struct {
	Name string `json:"name"`
}

// RegisterRoutes wires up /tagowners:
//
//   GET    /tagowners          => list all
//   GET    /tagowners/:name    => get one by name
//   POST   /tagowners          => create
//   PUT    /tagowners          => update
//   DELETE /tagowners          => delete
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

// listTagOwners => GET /tagOwners
// @Summary      List all tag owners
// @Description  Returns an array of TagOwner objects from state.
// @Tags         TagOwners
// @Accept       json
// @Produce      json
// @Success      200 {array}  TagOwner
// @Failure      500 {object} ErrorResponse "Failed to parse tagOwners"
// @Router       /tagOwners [get]
func listTagOwners(c *gin.Context, state *common.State) {
	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse tagOwners"})
		return
	}
	c.JSON(http.StatusOK, tagOwners)
}

// getTagOwnerByName => GET /tagOwners/:name
// @Summary      Get a tag owner by name
// @Description  Retrieves the TagOwner with the given name.
// @Tags         TagOwners
// @Accept       json
// @Produce      json
// @Param        name path string true "Tag name"
// @Success      200 {object} TagOwner
// @Failure      404 {object} ErrorResponse "TagOwner not found"
// @Failure      500 {object} ErrorResponse "Failed to parse tagOwners"
// @Router       /tagOwners/{name} [get]
func getTagOwnerByName(c *gin.Context, state *common.State) {
	name := c.Param("name")

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse tagOwners"})
		return
	}

	for _, t := range tagOwners {
		if t.Name == name {
			c.JSON(http.StatusOK, t)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "TagOwner not found"})
}

// createTagOwner => POST /tagOwners
// @Summary      Create a new tag owner
// @Description  Creates a TagOwner. Returns 409 if name already exists.
// @Tags         TagOwners
// @Accept       json
// @Produce      json
// @Param        tagOwner body TagOwner true "TagOwner to create"
// @Success      201 {object} TagOwner
// @Failure      400 {object} ErrorResponse "Bad request or missing name"
// @Failure      409 {object} ErrorResponse "TagOwner already exists"
// @Failure      500 {object} ErrorResponse "Failed to parse or save tagOwners"
// @Router       /tagOwners [post]
func createTagOwner(c *gin.Context, state *common.State) {
	var newTag TagOwner
	if err := c.ShouldBindJSON(&newTag); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if newTag.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse tagOwners"})
		return
	}

	// Check for conflict
	for _, t := range tagOwners {
		if t.Name == newTag.Name {
			c.JSON(http.StatusConflict, ErrorResponse{Error: "TagOwner already exists"})
			return
		}
	}

	tagOwners = append(tagOwners, newTag)
	if err := saveTagOwners(state, tagOwners); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new TagOwner"})
		return
	}
	c.JSON(http.StatusCreated, newTag)
}

// updateTagOwner => PUT /tagOwners
// @Summary      Update a tag owner
// @Description  Updates the TagOwner with a matching name. Expects JSON: { "name": "...", "owners": [...] }.
// @Tags         TagOwners
// @Accept       json
// @Produce      json
// @Param        tagOwner body TagOwner true "TagOwner to update"
// @Success      200 {object} TagOwner
// @Failure      400 {object} ErrorResponse "Bad request or missing name"
// @Failure      404 {object} ErrorResponse "TagOwner not found"
// @Failure      500 {object} ErrorResponse "Failed to parse or save changes"
// @Router       /tagOwners [put]
func updateTagOwner(c *gin.Context, state *common.State) {
	var updated TagOwner
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if updated.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse tagOwners"})
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
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "TagOwner not found"})
		return
	}

	if err := saveTagOwners(state, tagOwners); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update TagOwner"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteTagOwner => DELETE /tagowners
// @Summary      Delete a tag owner
// @Description  Expects JSON: { "name": "webserver" } to remove the matching TagOwner.
// @Tags         TagOwners
// @Accept       json
// @Produce      json
// @Param        body body deleteTagOwnerRequest true "Delete TagOwner request"
// @Success      200 {object} map[string]string "TagOwner deleted"
// @Failure      400 {object} ErrorResponse      "Bad request or missing name"
// @Failure      404 {object} ErrorResponse      "TagOwner not found"
// @Failure      500 {object} ErrorResponse      "Failed to save changes"
// @Router       /tagowners [delete]
func deleteTagOwner(c *gin.Context, state *common.State) {
	var req deleteTagOwnerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	tagOwners, err := getTagOwnersFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse tagOwners"})
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
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "TagOwner not found"})
		return
	}

	if err := saveTagOwners(state, tagOwners); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "TagOwner deleted"})
}

// -----------------------------------------------------------------------------
// Conversions between []TagOwner (API) and map[string][]string (actual storage):
//   "tagOwners": { "tag:<Name>": [ ...owners... ] }.
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
	// final stored data: map["tag:<name>"] => []string
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
