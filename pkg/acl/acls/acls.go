package acls

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// RegisterRoutes wires up ACL-related routes at /acls.
//
// For example:
//
//	GET /acls           => list all
//	GET /acls/:index    => get one by numeric index
//	POST /acls          => create a new ACL entry
//	PUT /acls           => update an existing ACL entry by index in JSON
//	DELETE /acls        => delete an ACL entry by index in JSON
func RegisterRoutes(r *gin.Engine, state *common.State) {
	a := r.Group("/acls")
	{
		a.GET("", func(c *gin.Context) {
			listACLs(c, state)
		})

		a.GET("/:index", func(c *gin.Context) {
			getACLByIndex(c, state)
		})

		a.POST("", func(c *gin.Context) {
			createACL(c, state)
		})

		a.PUT("", func(c *gin.Context) {
			updateACL(c, state)
		})

		a.DELETE("", func(c *gin.Context) {
			deleteACL(c, state)
		})
	}
}

// listACLs => GET /acls
func listACLs(c *gin.Context, state *common.State) {
	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
		return
	}
	c.JSON(http.StatusOK, acls)
}

// getACLByIndex => GET /acls/:index
func getACLByIndex(c *gin.Context, state *common.State) {
	indexStr := c.Param("index")
	i, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid index"})
		return
	}

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
		return
	}

	if i < 0 || i >= len(acls) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACL entry not found (index out of range)"})
		return
	}
	c.JSON(http.StatusOK, acls[i])
}

// createACL => POST /acls
func createACL(c *gin.Context, state *common.State) {
	var newACL tsclient.ACLEntry
	if err := c.ShouldBindJSON(&newACL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
		return
	}

	acls = append(acls, newACL)
	if err := state.UpdateKeyAndSave("acls", acls); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new ACL entry"})
		return
	}
	c.JSON(http.StatusCreated, newACL)
}

// updateACL => PUT /acls
// The user must provide JSON with an "index" field plus the new object fields.
func updateACL(c *gin.Context, state *common.State) {
	type updateRequest struct {
		Index int               `json:"index"`
		Entry tsclient.ACLEntry `json:"entry"`
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

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
		return
	}

	if req.Index >= len(acls) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACL entry not found (index out of range)"})
		return
	}

	// Replace the ACL entry at that index
	acls[req.Index] = req.Entry

	if err := state.UpdateKeyAndSave("acls", acls); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update ACL entry"})
		return
	}
	c.JSON(http.StatusOK, req.Entry)
}

// deleteACL => DELETE /acls
// The user must provide JSON with "index".
func deleteACL(c *gin.Context, state *common.State) {
	type deleteRequest struct {
		Index int `json:"index"`
	}
	var req deleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	acls, err := getACLsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
		return
	}
	if req.Index < 0 || req.Index >= len(acls) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACL entry not found (index out of range)"})
		return
	}

	acls = append(acls[:req.Index], acls[req.Index+1:]...)
	if err := state.UpdateKeyAndSave("acls", acls); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete ACL entry"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ACL entry deleted"})
}

// getACLsFromState re-marshal the generic state.Data["acls"] into []tsclient.ACLEntry
func getACLsFromState(state *common.State) ([]tsclient.ACLEntry, error) {
	raw := state.GetValue("acls") // uses RLock internally
	if raw == nil {
		return []tsclient.ACLEntry{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var acls []tsclient.ACLEntry
	if err := json.Unmarshal(b, &acls); err != nil {
		return nil, err
	}
	return acls, nil
}
