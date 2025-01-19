package ssh

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2" // where ACLSSH is defined
)

// RegisterRoutes wires up the SSH rules routes at /ssh.
//
// Example endpoints:
//
//	GET    /ssh         => list all SSH rules
//	GET    /ssh/:index  => get one by index
//	POST   /ssh         => create a new rule
//	PUT    /ssh         => update rule by index in JSON
//	DELETE /ssh         => delete rule by index in JSON
func RegisterRoutes(r *gin.Engine, state *common.State) {
	s := r.Group("/ssh")
	{
		s.GET("", func(c *gin.Context) {
			listSSH(c, state)
		})
		s.GET("/:index", func(c *gin.Context) {
			getSSHByIndex(c, state)
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
// returns all ACLSSH entries
func listSSH(c *gin.Context, state *common.State) {
	rules, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}
	c.JSON(http.StatusOK, rules)
}

// getSSHByIndex => GET /ssh/:index
func getSSHByIndex(c *gin.Context, state *common.State) {
	indexStr := c.Param("index")
	i, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid index"})
		return
	}

	rules, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	if i < 0 || i >= len(rules) {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule index out of range"})
		return
	}
	c.JSON(http.StatusOK, rules[i])
}

// createSSH => POST /ssh
// creates a new ACLSSH entry at the end of the slice
func createSSH(c *gin.Context, state *common.State) {
	var newRule tsclient.ACLSSH
	if err := c.ShouldBindJSON(&newRule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rules, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	rules = append(rules, newRule)
	if err := state.UpdateKeyAndSave("sshRules", rules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new SSH rule"})
		return
	}
	c.JSON(http.StatusCreated, newRule)
}

// updateSSH => PUT /ssh
// user must provide JSON with an "index" and the new rule data
func updateSSH(c *gin.Context, state *common.State) {
	type updateRequest struct {
		Index int             `json:"index"`
		Rule  tsclient.ACLSSH `json:"rule"`
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Index < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or missing 'index' field"})
		return
	}

	rules, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	if req.Index >= len(rules) {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule index out of range"})
		return
	}

	rules[req.Index] = req.Rule

	if err := state.UpdateKeyAndSave("sshRules", rules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update SSH rule"})
		return
	}
	c.JSON(http.StatusOK, req.Rule)
}

// deleteSSH => DELETE /ssh
// user must provide JSON with "index" to remove from the slice
func deleteSSH(c *gin.Context, state *common.State) {
	type deleteRequest struct {
		Index int `json:"index"`
	}
	var req deleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rules, err := getSSHFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
		return
	}

	if req.Index < 0 || req.Index >= len(rules) {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule index out of range"})
		return
	}

	rules = append(rules[:req.Index], rules[req.Index+1:]...)
	if err := state.UpdateKeyAndSave("sshRules", rules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete SSH rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "SSH rule deleted"})
}

// getSSHFromState => re-marshal state.Data["sshRules"] into []ACLSSH
func getSSHFromState(state *common.State) ([]tsclient.ACLSSH, error) {
	raw := state.GetValue("sshRules")
	if raw == nil {
		return []tsclient.ACLSSH{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var rules []tsclient.ACLSSH
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}
