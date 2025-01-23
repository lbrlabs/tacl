package ssh

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ACLSSH represents the user-facing SSH rule fields.
type ACLSSH struct {
	Action      string   `json:"action"`                // "accept" or "check"
	Src         []string `json:"src,omitempty"`         // list of sources
	Dst         []string `json:"dst,omitempty"`         // list of destinations
	Users       []string `json:"users,omitempty"`       // list of SSH users
	CheckPeriod string   `json:"checkPeriod,omitempty"` // optional, only for "check"
	AcceptEnv   []string `json:"acceptEnv,omitempty"`   // optional, allow-list environment variables
}

// ExtendedSSHEntry wraps ACLSSH with a stable unique ID.
type ExtendedSSHEntry struct {
	ID string `json:"id"` // stable UUID for each SSH rule
	ACLSSH
}

// RegisterRoutes wires up the SSH rules routes at /ssh.
//
//  GET     /ssh        => list all ExtendedSSHEntry
//  GET     /ssh/:id    => get by ID
//  POST    /ssh        => create (auto-generate ID)
//  PUT     /ssh        => update by ID in JSON
//  DELETE  /ssh        => delete by ID in JSON
func RegisterRoutes(r *gin.Engine, state *common.State) {
	s := r.Group("/ssh")
	{
		s.GET("", func(c *gin.Context) {
			listSSH(c, state)
		})
		s.GET("/:id", func(c *gin.Context) {
			getSSHByID(c, state)
		})
		s.POST("", func(c *gin.Context) {
			createSSH(c, state)
		})
		s.PUT("", func(c *gin.Context) {
			updateSSH(c, state)
		})
		s.DELETE("", func(c *gin.Context) {
			deleteSSH(c, state)
		})
	}
}

// listSSH => GET /ssh
// returns the entire slice of ExtendedSSHEntry
func listSSH(c *gin.Context, state *common.State) {
	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// getSSHByID => GET /ssh/:id
// fetches the ExtendedSSHEntry by UUID ID
func getSSHByID(c *gin.Context, state *common.State) {
	id := c.Param("id")

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	for _, entry := range entries {
		if entry.ID == id {
			c.JSON(http.StatusOK, entry)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule not found with that ID"})
}

// createSSH => POST /ssh
// appends a new ExtendedSSHEntry (with auto-generated ID) to the slice
func createSSH(c *gin.Context, state *common.State) {
	var newRule ACLSSH
	if err := c.ShouldBindJSON(&newRule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Basic validation
	if newRule.Action != "accept" && newRule.Action != "check" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action. Must be 'accept' or 'check'."})
		return
	}
	if newRule.Action == "check" {
		if _, err := time.ParseDuration(newRule.CheckPeriod); err != nil && newRule.CheckPeriod != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid checkPeriod. Must be a valid duration (e.g. '12h', '30m')."})
			return
		}
	}

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	// Create new ExtendedSSHEntry with a generated UUID.
	newEntry := ExtendedSSHEntry{
		ID:     uuid.NewString(),
		ACLSSH: newRule,
	}

	entries = append(entries, newEntry)
	if err := state.UpdateKeyAndSave("sshRules", entries); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new SSH rule"})
		return
	}
	c.JSON(http.StatusCreated, newEntry)
}

// updateSSH => PUT /ssh
// user must provide JSON like:
//   {
//     "id": "<uuid>",
//     "rule": { ... }
//   }
func updateSSH(c *gin.Context, state *common.State) {
	type updateRequest struct {
		ID   string `json:"id"`
		Rule ACLSSH `json:"rule"`
	}

	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'id' field"})
		return
	}

	// Basic validation on the new rule
	if req.Rule.Action != "accept" && req.Rule.Action != "check" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid action. Must be 'accept' or 'check'."})
		return
	}
	if req.Rule.Action == "check" && req.Rule.CheckPeriod == "" {
		req.Rule.CheckPeriod = "12h"
	}
	if req.Rule.Action == "check" {
		if _, err := time.ParseDuration(req.Rule.CheckPeriod); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid checkPeriod. Must be a valid duration."})
			return
		}
	}

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	// Find the entry with the matching ID
	var updated *ExtendedSSHEntry
	for i := range entries {
		if entries[i].ID == req.ID {
			// Found => update the embedded ACLSSH fields
			entries[i].ACLSSH = req.Rule
			updated = &entries[i]
			break
		}
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("sshRules", entries); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update SSH rule"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteSSH => DELETE /ssh
// user must provide JSON like:
//   {
//     "id": "<uuid>"
//   }
func deleteSSH(c *gin.Context, state *common.State) {
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

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	newList := make([]ExtendedSSHEntry, 0, len(entries))
	deleted := false
	for _, e := range entries {
		if e.ID == req.ID {
			deleted = true
			continue
		}
		newList = append(newList, e)
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("sshRules", newList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete SSH rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "SSH rule deleted"})
}

// getSSHFromState => re-marshal state.Data["sshRules"] into []ExtendedSSHEntry
func getSSHFromState(state *common.State) ([]ExtendedSSHEntry, error) {
	raw := state.GetValue("sshRules")
	if raw == nil {
		return []ExtendedSSHEntry{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var entries []ExtendedSSHEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
