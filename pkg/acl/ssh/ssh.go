package ssh

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse helps standardize error JSON in swagger docs.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ACLSSH represents the user-facing SSH rule fields.
// @Description ACLSSH defines fields for a single SSH rule, such as action, source, and destination.
type ACLSSH struct {
	// Action can be "accept" or "check".
	Action string `json:"action"`
	// Src is a list of source tags or CIDRs allowed by this SSH rule.
	Src []string `json:"src,omitempty"`
	// Dst is a list of destination tags or CIDRs for this SSH rule.
	Dst []string `json:"dst,omitempty"`
	// Users is a list of SSH users permitted by this rule.
	Users []string `json:"users,omitempty"`
	// CheckPeriod is only meaningful if Action == "check" (e.g. "12h", "30m").
	CheckPeriod string `json:"checkPeriod,omitempty"`
	// AcceptEnv is a list of environment variables allowed to pass through the SSH session.
	AcceptEnv []string `json:"acceptEnv,omitempty"`
}

// ExtendedSSHEntry wraps ACLSSH with a stable unique ID.
// @Description ExtendedSSHEntry is stored in TACL with a UUID "id" plus the SSH fields.
type ExtendedSSHEntry struct {
	// ID is a stable UUID for each SSH rule.
	ID string `json:"id"`
	ACLSSH
}

// UpdateRequest represents the JSON body for PUT /ssh:
// {
//   "id": "<uuid>",
//   "rule": { "action":"check","src":["tag:x"], ... }
// }
type UpdateRequest struct {
	ID   string `json:"id"`
	Rule ACLSSH `json:"rule"`
}

// DeleteRequest represents the JSON body for DELETE /ssh:
// { "id":"<uuid>" }
type DeleteRequest struct {
	ID string `json:"id"`
}

// RegisterRoutes wires up the SSH rules routes at /ssh.
//
//   GET     /ssh        => list all ExtendedSSHEntry
//   GET     /ssh/:id    => get by ID
//   POST    /ssh        => create (auto-generate ID)
//   PUT     /ssh        => update by ID in JSON
//   DELETE  /ssh        => delete by ID in JSON
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
// @Summary      List all SSH rules
// @Description  Returns the entire slice of ExtendedSSHEntry from state.
// @Tags         SSH
// @Accept       json
// @Produce      json
// @Success      200 {array}  ExtendedSSHEntry "List of SSH rules"
// @Failure      500 {object} ErrorResponse    "Failed to parse SSH rules"
// @Router       /ssh [get]
func listSSH(c *gin.Context, state *common.State) {
	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse SSH rules"})
		return
	}
	c.JSON(http.StatusOK, entries)
}

// getSSHByID => GET /ssh/:id
// @Summary      Get SSH rule by ID
// @Description  Retrieves a single ExtendedSSHEntry by its stable UUID.
// @Tags         SSH
// @Accept       json
// @Produce      json
// @Param        id path string true "UUID of the SSH rule"
// @Success      200 {object} ExtendedSSHEntry
// @Failure      404 {object} ErrorResponse "SSH rule not found with that ID"
// @Failure      500 {object} ErrorResponse "Failed to parse SSH rules"
// @Router       /ssh/{id} [get]
func getSSHByID(c *gin.Context, state *common.State) {
	id := c.Param("id")

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse SSH rules"})
		return
	}

	for _, entry := range entries {
		if entry.ID == id {
			c.JSON(http.StatusOK, entry)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "SSH rule not found with that ID"})
}

// createSSH => POST /ssh
// @Summary      Create a new SSH rule
// @Description  Appends a new ExtendedSSHEntry (with auto-generated ID) to the list of SSH rules.
// @Tags         SSH
// @Accept       json
// @Produce      json
// @Param        rule body ACLSSH true "SSH rule fields"
// @Success      201 {object} ExtendedSSHEntry
// @Failure      400 {object} ErrorResponse "Invalid JSON or fields"
// @Failure      500 {object} ErrorResponse "Failed to parse or save SSH rules"
// @Router       /ssh [post]
func createSSH(c *gin.Context, state *common.State) {
	var newRule ACLSSH
	if err := c.ShouldBindJSON(&newRule); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Basic validation
	if newRule.Action != "accept" && newRule.Action != "check" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid action. Must be 'accept' or 'check'."})
		return
	}
	if newRule.Action == "check" && newRule.CheckPeriod != "" {
		if _, err := time.ParseDuration(newRule.CheckPeriod); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid checkPeriod. Must be a valid duration (e.g. '12h', '30m')."})
			return
		}
	}

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse SSH rules"})
		return
	}

	newEntry := ExtendedSSHEntry{
		ID:     uuid.NewString(),
		ACLSSH: newRule,
	}

	// Append to the "ssh" array
	entries = append(entries, newEntry)

	if err := state.UpdateKeyAndSave("ssh", entries); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new SSH rule"})
		return
	}
	c.JSON(http.StatusCreated, newEntry)
}

// updateSSH => PUT /ssh
// @Summary      Update an existing SSH rule
// @Description  User must provide JSON like { "id":"<uuid>", "rule": {...} } to replace the rule with matching ID.
// @Tags         SSH
// @Accept       json
// @Produce      json
// @Param        body body UpdateRequest true "Update SSH request body"
// @Success      200 {object} ExtendedSSHEntry
// @Failure      400 {object} ErrorResponse "Bad request or missing fields"
// @Failure      404 {object} ErrorResponse "SSH rule not found with that ID"
// @Failure      500 {object} ErrorResponse "Failed to parse or update SSH rule"
// @Router       /ssh [put]
func updateSSH(c *gin.Context, state *common.State) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'id' field"})
		return
	}

	// Basic validation on the new rule
	if req.Rule.Action != "accept" && req.Rule.Action != "check" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid action. Must be 'accept' or 'check'."})
		return
	}
	if req.Rule.Action == "check" {
		// If no CheckPeriod is set, default to "12h"
		if req.Rule.CheckPeriod == "" {
			req.Rule.CheckPeriod = "12h"
		}
		if _, err := time.ParseDuration(req.Rule.CheckPeriod); err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid checkPeriod. Must be a valid duration."})
			return
		}
	}

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse SSH rules"})
		return
	}

	var updated *ExtendedSSHEntry
	for i := range entries {
		if entries[i].ID == req.ID {
			entries[i].ACLSSH = req.Rule
			updated = &entries[i]
			break
		}
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "SSH rule not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("ssh", entries); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update SSH rule"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteSSH => DELETE /ssh
// @Summary      Delete an SSH rule
// @Description  User must provide JSON like { "id":"<uuid>" } to remove the rule with matching ID.
// @Tags         SSH
// @Accept       json
// @Produce      json
// @Param        body body DeleteRequest true "Delete SSH rule request"
// @Success      200 {object} map[string]string "SSH rule deleted"
// @Failure      400 {object} ErrorResponse "Missing or invalid ID"
// @Failure      404 {object} ErrorResponse "SSH rule not found with that ID"
// @Failure      500 {object} ErrorResponse "Failed to delete SSH rule"
// @Router       /ssh [delete]
func deleteSSH(c *gin.Context, state *common.State) {
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'id' field"})
		return
	}

	entries, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse SSH rules"})
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
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "SSH rule not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("ssh", newList); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete SSH rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "SSH rule deleted"})
}

// getSSHFromState => re-marshal state.Data["ssh"] into []ExtendedSSHEntry
func getSSHFromState(state *common.State) ([]ExtendedSSHEntry, error) {
	raw := state.GetValue("ssh")
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
