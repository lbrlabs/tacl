package ssh

import (
    "encoding/json"
    "net/http"
    "strconv"
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
func RegisterRoutes(r *gin.Engine, state *common.State) {
    s := r.Group("/ssh")
    {
        s.GET("", func(c *gin.Context) {
            listSSH(c, state)
        })
        // GET /ssh/:index => get by numeric index
        s.GET("/:index", func(c *gin.Context) {
            getSSHByIndex(c, state)
        })
        // POST /ssh => create (automatically generate ID)
        s.POST("", func(c *gin.Context) {
            createSSH(c, state)
        })
        // PUT /ssh => update by index in JSON
        s.PUT("", func(c *gin.Context) {
            updateSSH(c, state)
        })
        // DELETE /ssh => delete by index in JSON
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

// getSSHByIndex => GET /ssh/:index
// fetches the ExtendedSSHEntry by array index
func getSSHByIndex(c *gin.Context, state *common.State) {
    indexStr := c.Param("index")
    i, err := strconv.Atoi(indexStr)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid index"})
        return
    }

    entries, err := getSSHFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
        return
    }

    if i < 0 || i >= len(entries) {
        c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule index out of range"})
        return
    }
    c.JSON(http.StatusOK, entries[i])
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
        if _, err := time.ParseDuration(newRule.CheckPeriod); err != nil {
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
// user must provide JSON with an "index" and the new rule data => { "index": 0, "rule": { ... } }
func updateSSH(c *gin.Context, state *common.State) {
    type updateRequest struct {
        Index int    `json:"index"`
        Rule  ACLSSH `json:"rule"`
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

    if req.Index >= len(entries) {
        c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule index out of range"})
        return
    }

    // Keep the existing ID the same, only update the embedded ACLSSH fields
    entries[req.Index].ACLSSH = req.Rule

    if err := state.UpdateKeyAndSave("sshRules", entries); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update SSH rule"})
        return
    }
    c.JSON(http.StatusOK, entries[req.Index])
}

// deleteSSH => DELETE /ssh
// user must provide JSON with "index" => { "index": 0 }
func deleteSSH(c *gin.Context, state *common.State) {
    type deleteRequest struct {
        Index int `json:"index"`
    }
    var req deleteRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    entries, err := getSSHFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse SSH rules"})
        return
    }

    if req.Index < 0 || req.Index >= len(entries) {
        c.JSON(http.StatusNotFound, gin.H{"error": "SSH rule index out of range"})
        return
    }

    entries = append(entries[:req.Index], entries[req.Index+1:]...)
    if err := state.UpdateKeyAndSave("sshRules", entries); err != nil {
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
