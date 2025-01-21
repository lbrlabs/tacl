// pkg/acl/hosts/hosts.go

package hosts

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	"net/http"
)

// Host is the user-facing structure.
// The final stored JSON is map["hostname"] => "ipString".
type Host struct {
	Name string `json:"name" binding:"required"`
	IP   string `json:"ip"   binding:"required"`
}

// RegisterRoutes wires up the /hosts endpoints.
// The final JSON in state.Data["hosts"] is map["Name"] => "IP/CIDR".
func RegisterRoutes(r *gin.Engine, state *common.State) {
	h := r.Group("/hosts")
	{
		// GET /hosts => list all
		h.GET("", func(c *gin.Context) {
			listHosts(c, state)
		})

		// GET /hosts/:name => get one host
		h.GET("/:name", func(c *gin.Context) {
			getHostByName(c, state)
		})

		// POST /hosts => create
		h.POST("", func(c *gin.Context) {
			createHost(c, state)
		})

		// PUT /hosts => update
		h.PUT("", func(c *gin.Context) {
			updateHost(c, state)
		})

		// DELETE /hosts => delete
		h.DELETE("", func(c *gin.Context) {
			deleteHost(c, state)
		})
	}
}

// listHosts => GET /hosts
// Returns an array of Host objects. The final data is a map, so we convert it back to an array.
func listHosts(c *gin.Context, state *common.State) {
	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse hosts"})
		return
	}
	c.JSON(http.StatusOK, hosts)
}

// getHostByName => GET /hosts/:name
func getHostByName(c *gin.Context, state *common.State) {
	name := c.Param("name")

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse hosts"})
		return
	}

	for _, h := range hosts {
		if h.Name == name {
			c.JSON(http.StatusOK, h)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
}

// createHost => POST /hosts
// Returns 409 Conflict if the name already exists.
func createHost(c *gin.Context, state *common.State) {
	var newHost Host
	if err := c.ShouldBindJSON(&newHost); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if newHost.Name == "" || newHost.IP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' or 'ip' field"})
		return
	}

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse hosts"})
		return
	}

	// Check for conflict
	for _, h := range hosts {
		if h.Name == newHost.Name {
			c.JSON(http.StatusConflict, gin.H{"error": "Host already exists"})
			return
		}
	}

	hosts = append(hosts, newHost)
	if err := saveHosts(state, hosts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new host"})
		return
	}
	c.JSON(http.StatusCreated, newHost)
}

// updateHost => PUT /hosts
// Expects { "name": "...", "ip": "..." }
func updateHost(c *gin.Context, state *common.State) {
	var updated Host
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if updated.Name == "" || updated.IP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'name' or 'ip' field"})
		return
	}

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse hosts"})
		return
	}

	found := false
	for i, h := range hosts {
		if h.Name == updated.Name {
			hosts[i] = updated
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	if err := saveHosts(state, hosts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update host"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteHost => DELETE /hosts
// Expects JSON: { "name": "example-host-1" }
func deleteHost(c *gin.Context, state *common.State) {
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

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse hosts"})
		return
	}

	found := false
	for i, h := range hosts {
		if h.Name == req.Name {
			hosts = append(hosts[:i], hosts[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Host not found"})
		return
	}

	if err := saveHosts(state, hosts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Host deleted"})
}

// -----------------------------------------------------------------------------
// We want final JSON => "hosts": { "<Name>":"<IP>", ... }
// But user sees array of Host {Name, IP}.
//
// getHostsFromState => read map => convert to []Host
// saveHosts => convert []Host => map => store
// -----------------------------------------------------------------------------

func getHostsFromState(state *common.State) ([]Host, error) {
	raw := state.GetValue("hosts")
	if raw == nil {
		return []Host{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	// final stored data: map["Name"] => "IP"
	var rawMap map[string]string
	if err := json.Unmarshal(b, &rawMap); err != nil {
		return nil, err
	}

	// Convert map => array
	var out []Host
	for name, ip := range rawMap {
		out = append(out, Host{
			Name: name,
			IP:   ip,
		})
	}
	return out, nil
}

func saveHosts(state *common.State, hosts []Host) error {
	m := make(map[string]string)
	for _, h := range hosts {
		m[h.Name] = h.IP
	}
	return state.UpdateKeyAndSave("hosts", m)
}
