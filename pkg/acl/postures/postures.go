package postures

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse is used to provide a consistent error output in Swagger docs.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Posture represents a named posture entry and its list of rules.
//
// Example JSON:
//
//	{
//	  "name": "latestMac",
//	  "rules": [
//	    "node:os in ['macos']",
//	    "node:tsVersion >= '1.40'"
//	  ]
//	}
//
// @Description Posture defines a named posture with a list of rule expressions.
type Posture struct {
	// Name is the unique name of this posture.
	Name string `json:"name" binding:"required"`
	// Rules is a list of string expressions describing posture requirements.
	Rules []string `json:"rules"`
}

// DeletePostureRequest is the shape of the JSON body for DELETE /postures.
type DeletePostureRequest struct {
	Name string `json:"name"`
}

// DefaultPostureBody is used for PUT /postures/default when setting the default posture.
//
// Example:
//  {
//    "defaultSourcePosture": [ "some expression", "another rule" ]
//  }
type DefaultPostureBody struct {
	DefaultSourcePosture []string `json:"defaultSourcePosture"`
}

// listAllResponse represents the structure returned by GET /postures.
type listAllResponse struct {
	DefaultSourcePosture []string  `json:"defaultSourcePosture"`
	Items                []Posture `json:"items"`
}

// RegisterRoutes wires up /postures, including:
//   - Named posture CRUD
//   - Default posture GET/PUT/DELETE at /postures/default
//
// The final stored data in state.Data["postures"] is a map:
//   - "posture:<NAME>" => []string (the named posture rules)
//   - "defaultSourcePosture" => []string (the global default posture rules)
func RegisterRoutes(r *gin.Engine, state *common.State) {
	p := r.Group("/postures")
	{
		// GET /postures => list all
		p.GET("", func(c *gin.Context) {
			listAllPostures(c, state)
		})

		// GET /postures/:name => get one posture OR the default
		p.GET("/:name", func(c *gin.Context) {
			name := c.Param("name")
			if name == "default" {
				getDefaultPosture(c, state)
			} else {
				getPostureByName(c, state, name)
			}
		})

		// POST /postures => create
		p.POST("", func(c *gin.Context) {
			createPosture(c, state)
		})

		// PUT /postures => update
		p.PUT("", func(c *gin.Context) {
			updatePosture(c, state)
		})

		// DELETE /postures => delete
		p.DELETE("", func(c *gin.Context) {
			deletePosture(c, state)
		})

		// Default posture: GET/PUT/DELETE => /postures/default
		p.GET("/default", func(c *gin.Context) {
			getDefaultPosture(c, state)
		})
		p.PUT("/default", func(c *gin.Context) {
			setDefaultPosture(c, state)
		})
		p.DELETE("/default", func(c *gin.Context) {
			deleteDefaultPosture(c, state)
		})
	}
}

// -----------------------------------------------------------------------------
// Named Posture List
// -----------------------------------------------------------------------------

// listAllPostures => GET /postures
// @Summary      List all named postures + default
// @Description  Returns an object containing "defaultSourcePosture" and an array of named "items".
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Success      200 {object} listAllResponse
// @Failure      500 {object} ErrorResponse "Failed to parse or load postures"
// @Router       /postures [get]
func listAllPostures(c *gin.Context, state *common.State) {
	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, listAllResponse{
		DefaultSourcePosture: defaultPosture,
		Items:                postures,
	})
}

// getPostureByName => GET /postures/:name (when :name != "default")
// @Summary      Get posture by name
// @Description  Retrieves a single posture object by its name (e.g. "latestMac").
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Param        name path string true "Name of the posture"
// @Success      200 {object} Posture
// @Failure      404 {object} ErrorResponse "Posture not found"
// @Failure      500 {object} ErrorResponse "Failed to parse or load postures"
// @Router       /postures/{name} [get]
func getPostureByName(c *gin.Context, state *common.State, name string) {
	postures, _, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	for _, p := range postures {
		if p.Name == name {
			c.JSON(http.StatusOK, p)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "Posture not found"})
}

// createPosture => POST /postures
// @Summary      Create a new posture
// @Description  Creates a posture with unique name. Returns 409 if that name already exists.
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Param        posture body Posture true "Posture to create"
// @Success      201 {object} Posture
// @Failure      400 {object} ErrorResponse "Bad request or missing name"
// @Failure      409 {object} ErrorResponse "Posture already exists"
// @Failure      500 {object} ErrorResponse "Failed to parse or save postures"
// @Router       /postures [post]
func createPosture(c *gin.Context, state *common.State) {
	var newPosture Posture
	if err := c.ShouldBindJSON(&newPosture); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if newPosture.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// Check conflict
	for _, p := range postures {
		if p.Name == newPosture.Name {
			c.JSON(http.StatusConflict, ErrorResponse{Error: "Posture already exists"})
			return
		}
	}

	// Append & save
	postures = append(postures, newPosture)
	if err := savePosturesAndDefault(state, postures, defaultPosture); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new posture"})
		return
	}
	c.JSON(http.StatusCreated, newPosture)
}

// updatePosture => PUT /postures
// @Summary      Update a posture
// @Description  Updates the posture by matching on its name. Returns 404 if not found.
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Param        posture body Posture true "Posture with updated rules"
// @Success      200 {object} Posture
// @Failure      400 {object} ErrorResponse "Missing fields"
// @Failure      404 {object} ErrorResponse "Posture not found"
// @Failure      500 {object} ErrorResponse "Failed to update posture"
// @Router       /postures [put]
func updatePosture(c *gin.Context, state *common.State) {
	var updated Posture
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if updated.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	found := false
	for i, p := range postures {
		if p.Name == updated.Name {
			postures[i] = updated
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Posture not found"})
		return
	}

	if err := savePosturesAndDefault(state, postures, defaultPosture); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update posture"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deletePosture => DELETE /postures
// @Summary      Delete a posture
// @Description  Deletes a named posture by JSON body. Expects { "name": "<postureName>" }.
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Param        body body DeletePostureRequest true "Delete posture request"
// @Success      200 {object} map[string]string "Posture deleted"
// @Failure      400 {object} ErrorResponse "Bad request or missing name"
// @Failure      404 {object} ErrorResponse "Posture not found"
// @Failure      500 {object} ErrorResponse "Failed to save changes"
// @Router       /postures [delete]
func deletePosture(c *gin.Context, state *common.State) {
	var req DeletePostureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	found := false
	for i, p := range postures {
		if p.Name == req.Name {
			postures = append(postures[:i], postures[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Posture not found"})
		return
	}

	if err := savePosturesAndDefault(state, postures, defaultPosture); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Posture deleted"})
}

// getDefaultPosture => GET /postures/default
// @Summary      Get the default posture
// @Description  Returns the "defaultSourcePosture" array if set, otherwise an empty array or 200 with none.
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string][]string "defaultSourcePosture: []string"
// @Failure      500 {object} ErrorResponse "Failed to parse or load postures"
// @Router       /postures/default [get]
func getDefaultPosture(c *gin.Context, state *common.State) {
	_, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"defaultSourcePosture": defaultPosture})
}

// setDefaultPosture => PUT /postures/default
// @Summary      Set the default posture
// @Description  Overwrites the default posture with the given array of rules.
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Param        body body DefaultPostureBody true "Default posture array"
// @Success      200 {object} map[string][]string "defaultSourcePosture: updated array"
// @Failure      400 {object} ErrorResponse "Bad request"
// @Failure      500 {object} ErrorResponse "Failed to set default posture"
// @Router       /postures/default [put]
func setDefaultPosture(c *gin.Context, state *common.State) {
	var body DefaultPostureBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	dsp := body.DefaultSourcePosture

	postures, _, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	if err := savePosturesAndDefault(state, postures, dsp); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to set default posture"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"defaultSourcePosture": dsp})
}

// deleteDefaultPosture => DELETE /postures/default
// @Summary      Delete the default posture
// @Description  Removes any default posture rules by setting them to nil.
// @Tags         Postures
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]string "defaultSourcePosture removed"
// @Failure      500 {object} ErrorResponse "Failed to delete default posture"
// @Router       /postures/default [delete]
func deleteDefaultPosture(c *gin.Context, state *common.State) {
	postures, _, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if err := savePosturesAndDefault(state, postures, nil); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete default posture"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "defaultSourcePosture removed"})
}

// -----------------------------------------------------------------------------
// Internal storage format in state.Data["postures"] => map[string][]string
//   - "posture:<NAME>" => []string (the named posture rules)
//   - "defaultSourcePosture" => []string (the global default posture rules)
// -----------------------------------------------------------------------------

// getPosturesAndDefault => read map from state => parse out named postures + default
func getPosturesAndDefault(state *common.State) (postureList []Posture, defaultPosture []string, err error) {
	raw := state.GetValue("postures")
	if raw == nil {
		return []Posture{}, nil, nil
	}
	b, e := json.Marshal(raw)
	if e != nil {
		return nil, nil, e
	}
	var rawMap map[string][]string
	if e := json.Unmarshal(b, &rawMap); e != nil {
		return nil, nil, e
	}

	// Convert map => postureList
	var out []Posture
	var dsp []string
	for k, v := range rawMap {
		if k == "defaultSourcePosture" {
			dsp = v
			continue
		}
		// strip leading "posture:" if present
		name := strings.TrimPrefix(k, "posture:")
		out = append(out, Posture{
			Name:  name,
			Rules: v,
		})
	}
	return out, dsp, nil
}

// savePosturesAndDefault => convert postureList + default => map => write to state
func savePosturesAndDefault(state *common.State, postures []Posture, defaultPosture []string) error {
	m := make(map[string][]string)

	// Insert named postures
	for _, p := range postures {
		key := p.Name
		if !strings.HasPrefix(key, "posture:") {
			key = "posture:" + key
		}
		m[key] = p.Rules
	}

	// Insert default posture if set
	if len(defaultPosture) > 0 {
		m["defaultSourcePosture"] = defaultPosture
	}

	return state.UpdateKeyAndSave("postures", m)
}
