package postures

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// Posture represents a named "posture" entry and its list of rules.
// Example JSON when creating/updating:
//
//   {
//     "name": "latestMac",
//     "rules": [
//       "node:os in ['macos']",
//       "node:tsVersion >= '1.40'"
//     ]
//   }
type Posture struct {
	Name  string   `json:"name" binding:"required"`
	Rules []string `json:"rules"`
}

// RegisterRoutes wires up /postures, including:
// - Named posture CRUD (like /groups, /tagowners)
// - Default posture GET/PUT/DELETE at /postures/default
func RegisterRoutes(r *gin.Engine, state *common.State) {
	p := r.Group("/postures")
	{
		// Entire list of named Posture + the current default
		p.GET("", func(c *gin.Context) {
			listAllPostures(c, state)
		})

		// Named posture CRUD
		p.GET("/:name", func(c *gin.Context) {
			name := c.Param("name")
			if name == "default" {
				// conflict with our default route
				getDefaultPosture(c, state)
			} else {
				getPostureByName(c, state, name)
			}
		})
		p.POST("", func(c *gin.Context) {
			createPosture(c, state)
		})
		p.PUT("", func(c *gin.Context) {
			updatePosture(c, state)
		})
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
// Returns a JSON object containing:
// {
//   "defaultSourcePosture": [...],
//   "items": [
//      { "name":"latestMac", "rules": [...] },
//      ...
//   ]
// }
func listAllPostures(c *gin.Context, state *common.State) {
	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"defaultSourcePosture": defaultPosture,
		"items":                postures,
	})
}

// getPostureByName => GET /postures/:name (when :name != "default")
func getPostureByName(c *gin.Context, state *common.State, name string) {
	postures, _, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, p := range postures {
		if p.Name == name {
			c.JSON(http.StatusOK, p)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Posture not found"})
}

// createPosture => POST /postures
// 409 Conflict if the name already exists
func createPosture(c *gin.Context, state *common.State) {
	var newPosture Posture
	if err := c.ShouldBindJSON(&newPosture); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if newPosture.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Check for conflict
	for _, p := range postures {
		if p.Name == newPosture.Name {
			c.JSON(http.StatusConflict, gin.H{"error": "Posture already exists"})
			return
		}
	}

	// Append & save
	postures = append(postures, newPosture)
	if err := savePosturesAndDefault(state, postures, defaultPosture); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new posture"})
		return
	}
	c.JSON(http.StatusCreated, newPosture)
}

// updatePosture => PUT /postures
// Expects { "name": "...", "rules": [...] }
func updatePosture(c *gin.Context, state *common.State) {
	var updated Posture
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if updated.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' field"})
		return
	}

	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Posture not found"})
		return
	}

	if err := savePosturesAndDefault(state, postures, defaultPosture); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update posture"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deletePosture => DELETE /postures
// Expects { "name": "latestMac" }
func deletePosture(c *gin.Context, state *common.State) {
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

	postures, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "Posture not found"})
		return
	}

	if err := savePosturesAndDefault(state, postures, defaultPosture); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Posture deleted"})
}

// -----------------------------------------------------------------------------
// DefaultSourcePosture (special "defaultSourcePosture" key in the map)
// -----------------------------------------------------------------------------

// getDefaultPosture => GET /postures/default
func getDefaultPosture(c *gin.Context, state *common.State) {
	_, defaultPosture, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"defaultSourcePosture": defaultPosture})
}

// setDefaultPosture => PUT /postures/default
// Expects JSON: { "defaultSourcePosture": [ "some expression", ... ] }
func setDefaultPosture(c *gin.Context, state *common.State) {
	var body struct {
		DefaultSourcePosture []string `json:"defaultSourcePosture"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// If user didn't pass defaultSourcePosture or set it to nil, treat as empty?
	dsp := body.DefaultSourcePosture

	postures, _, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := savePosturesAndDefault(state, postures, dsp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set default posture"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"defaultSourcePosture": dsp})
}

// deleteDefaultPosture => DELETE /postures/default
func deleteDefaultPosture(c *gin.Context, state *common.State) {
	postures, _, err := getPosturesAndDefault(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Setting it to nil or empty effectively deletes it
	if err := savePosturesAndDefault(state, postures, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete default posture"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "defaultSourcePosture removed"})
}

// -----------------------------------------------------------------------------
// Internal storage format in state.Data["postures"] => map[string][]string
//  - "posture:<NAME>" => []string (the named posture rules)
//  - "defaultSourcePosture" => []string (the global default posture rules)
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

// savePosturesAndDefault => convert postureList + default => map => write to state.
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
