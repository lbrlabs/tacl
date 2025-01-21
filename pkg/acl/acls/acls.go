// pkg/acl/acls/acls.go
package acls

import (
    "encoding/json"
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "github.com/lbrlabs/tacl/pkg/common"
    tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// ExtendedACLEntry => local storage, stable ID + the usual ACL fields
type ExtendedACLEntry struct {
    ID string `json:"id"` // stable UUID

    // Embed Tailscale's ACLEntry fields. That has "Action", "Src", "Dst", etc.
    tsclient.ACLEntry
}

// RegisterRoutes wires up ACL-related routes at /acls:
//
//   GET /acls           => list all (by ID)
//   GET /acls/:id       => get one by ID
//   POST /acls          => create (generate a new ID)
//   PUT /acls           => update an existing ACL by ID in JSON
//   DELETE /acls        => delete by ID in JSON
//
func RegisterRoutes(r *gin.Engine, state *common.State) {
    a := r.Group("/acls")
    {
        a.GET("", func(c *gin.Context) {
            listACLs(c, state)
        })

        a.GET("/:id", func(c *gin.Context) {
            getACLByID(c, state)
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

// listACLs => GET /acls => returns entire []ExtendedACLEntry
func listACLs(c *gin.Context, state *common.State) {
    acls, err := getACLsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
        return
    }
    c.JSON(http.StatusOK, acls)
}

// getACLByID => GET /acls/:id
func getACLByID(c *gin.Context, state *common.State) {
    id := c.Param("id")

    acls, err := getACLsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
        return
    }

    for _, entry := range acls {
        if entry.ID == id {
            c.JSON(http.StatusOK, entry)
            return
        }
    }
    c.JSON(http.StatusNotFound, gin.H{"error": "ACL entry not found"})
}

// createACL => POST /acls
// Body => Tailscale's ACLEntry fields (action, src, dst, etc). We'll embed them in ExtendedACLEntry w/ a new ID.
func createACL(c *gin.Context, state *common.State) {
    var newData tsclient.ACLEntry
    if err := c.ShouldBindJSON(&newData); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    acls, err := getACLsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
        return
    }

    newEntry := ExtendedACLEntry{
        ID:        uuid.NewString(),
        ACLEntry:  newData,
    }

    acls = append(acls, newEntry)
    if err := state.UpdateKeyAndSave("acls", acls); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new ACL entry"})
        return
    }
    c.JSON(http.StatusCreated, newEntry)
}

// updateACL => PUT /acls => body shape:
//
//   {
//     "id": "...",
//     "entry": {
//       "action": "...",
//       "src": [...],
//       "dst": [...]
//     }
//   }
//
func updateACL(c *gin.Context, state *common.State) {
    type updateRequest struct {
        ID    string             `json:"id"`
        Entry tsclient.ACLEntry  `json:"entry"`
    }
    var req updateRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    if req.ID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing ACL 'id' in request body"})
        return
    }

    acls, err := getACLsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
        return
    }

    var updated *ExtendedACLEntry
    for i := range acls {
        if acls[i].ID == req.ID {
            // Found => update the embedded ACLEntry
            acls[i].ACLEntry = req.Entry
            updated = &acls[i]
            break
        }
    }
    if updated == nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "ACL entry not found"})
        return
    }

    if err := state.UpdateKeyAndSave("acls", acls); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update ACL entry"})
        return
    }
    c.JSON(http.StatusOK, updated)
}

// deleteACL => DELETE /acls => body => { "id": "<uuid>" }
func deleteACL(c *gin.Context, state *common.State) {
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

    acls, err := getACLsFromState(state)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLs"})
        return
    }

    newList := make([]ExtendedACLEntry, 0, len(acls))
    deleted := false
    for _, entry := range acls {
        if entry.ID == req.ID {
            // skip it => effectively remove
            deleted = true
            continue
        }
        newList = append(newList, entry)
    }
    if !deleted {
        c.JSON(http.StatusNotFound, gin.H{"error": "ACL entry not found with that ID"})
        return
    }

    if err := state.UpdateKeyAndSave("acls", newList); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete ACL entry"})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": "ACL entry deleted"})
}

// getACLsFromState => read state.Data["acls"] => []ExtendedACLEntry
func getACLsFromState(state *common.State) ([]ExtendedACLEntry, error) {
    raw := state.GetValue("acls")
    if raw == nil {
        return []ExtendedACLEntry{}, nil
    }
    b, err := json.Marshal(raw)
    if err != nil {
        return nil, err
    }
    var acls []ExtendedACLEntry
    if err := json.Unmarshal(b, &acls); err != nil {
        return nil, err
    }
    return acls, nil
}
