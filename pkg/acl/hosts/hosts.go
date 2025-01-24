// pkg/acl/hosts/hosts.go
package hosts

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse describes the error object returned in case of failures.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Host is the user-facing structure for a hostname -> IP mapping.
// The final stored JSON is map["Name"] => "IP".
//
// @Description Host has a required name (hostname) and IP.
type Host struct {
	// Name is the hostname identifier.
	Name string `json:"name" binding:"required"`
	// IP is the IP or CIDR address associated with this hostname.
	IP   string `json:"ip"   binding:"required"`
}

// DeleteHostRequest is the JSON body for DELETE /hosts.
// Example: { "name": "example-host" }
type DeleteHostRequest struct {
	Name string `json:"name"`
}

// RegisterRoutes wires up the /hosts endpoints.
//
//   GET    /hosts       => list all hosts
//   GET    /hosts/:name => get one host by name
//   POST   /hosts       => create a new host
//   PUT    /hosts       => update an existing host
//   DELETE /hosts       => delete a host
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
// @Summary      List all hosts
// @Description  Returns an array of Host objects. The final data is a map in storage, converted back to an array.
// @Tags         Hosts
// @Accept       json
// @Produce      json
// @Success      200 {array}  Host
// @Failure      500 {object} ErrorResponse "Failed to parse hosts"
// @Router       /hosts [get]
func listHosts(c *gin.Context, state *common.State) {
	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse hosts"})
		return
	}
	c.JSON(http.StatusOK, hosts)
}

// getHostByName => GET /hosts/:name
// @Summary      Get a host by name
// @Description  Retrieves a single host by its name (hostname).
// @Tags         Hosts
// @Accept       json
// @Produce      json
// @Param        name path string true "Hostname"
// @Success      200  {object} Host
// @Failure      404  {object} ErrorResponse "Host not found"
// @Failure      500  {object} ErrorResponse "Failed to parse hosts"
// @Router       /hosts/{name} [get]
func getHostByName(c *gin.Context, state *common.State) {
	name := c.Param("name")

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse hosts"})
		return
	}

	for _, h := range hosts {
		if h.Name == name {
			c.JSON(http.StatusOK, h)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "Host not found"})
}

// createHost => POST /hosts
// @Summary      Create a new host
// @Description  Creates a host mapping from name to IP. Returns 409 if the hostname already exists.
// @Tags         Hosts
// @Accept       json
// @Produce      json
// @Param        host body Host true "Host to create"
// @Success      201 {object} Host
// @Failure      400 {object} ErrorResponse "Bad request or missing fields"
// @Failure      409 {object} ErrorResponse "Host already exists"
// @Failure      500 {object} ErrorResponse "Failed to parse or save hosts"
// @Router       /hosts [post]
func createHost(c *gin.Context, state *common.State) {
	var newHost Host
	if err := c.ShouldBindJSON(&newHost); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if newHost.Name == "" || newHost.IP == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' or 'ip' field"})
		return
	}

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse hosts"})
		return
	}

	for _, h := range hosts {
		if h.Name == newHost.Name {
			c.JSON(http.StatusConflict, ErrorResponse{Error: "Host already exists"})
			return
		}
	}

	hosts = append(hosts, newHost)
	if err := saveHosts(state, hosts); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new host"})
		return
	}
	c.JSON(http.StatusCreated, newHost)
}

// updateHost => PUT /hosts
// @Summary      Update an existing host
// @Description  Updates the IP for a host by matching the 'name'. Returns 404 if not found.
// @Tags         Hosts
// @Accept       json
// @Produce      json
// @Param        host body Host true "Updated host info"
// @Success      200 {object} Host
// @Failure      400 {object} ErrorResponse "Bad request or missing fields"
// @Failure      404 {object} ErrorResponse "Host not found"
// @Failure      500 {object} ErrorResponse "Failed to update host"
// @Router       /hosts [put]
func updateHost(c *gin.Context, state *common.State) {
	var updated Host
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if updated.Name == "" || updated.IP == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' or 'ip' field"})
		return
	}

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse hosts"})
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
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Host not found"})
		return
	}

	if err := saveHosts(state, hosts); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update host"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteHost => DELETE /hosts
// @Summary      Delete a host
// @Description  Deletes a host by name, based on JSON input { "name": "..." }.
// @Tags         Hosts
// @Accept       json
// @Produce      json
// @Param        body body DeleteHostRequest true "Delete host request"
// @Success      200 {object} map[string]string "Host deleted"
// @Failure      400 {object} ErrorResponse     "Missing name"
// @Failure      404 {object} ErrorResponse     "Host not found"
// @Failure      500 {object} ErrorResponse     "Failed to save changes"
// @Router       /hosts [delete]
func deleteHost(c *gin.Context, state *common.State) {
	var req DeleteHostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'name' field"})
		return
	}

	hosts, err := getHostsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse hosts"})
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
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Host not found"})
		return
	}

	if err := saveHosts(state, hosts); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save changes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Host deleted"})
}

// -----------------------------------------------------------------------------
// The final JSON in state.Data["hosts"] is map["Name"] => "IP/CIDR".
// We convert between that map and []Host for the user-facing endpoints.
// -----------------------------------------------------------------------------

// getHostsFromState => read the map => convert to []Host
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

// saveHosts => convert []Host => map => store
func saveHosts(state *common.State, hosts []Host) error {
	m := make(map[string]string)
	for _, h := range hosts {
		m[h.Name] = h.IP
	}
	return state.UpdateKeyAndSave("hosts", m)
}
