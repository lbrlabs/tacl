package derpmap

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"

	// Tailscale's types, used at runtime but not directly in Swag references:
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// ErrorResponse is used in @Failure annotations for descriptive error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ACLDERPNodeDoc represents a DERP node in the doc for swagger/JSON I/O.
type ACLDERPNodeDoc struct {
	Name     string `json:"name,omitempty"`
	RegionID int    `json:"regionID,omitempty"`
	HostName string `json:"hostName,omitempty"`
	IPv4     string `json:"ipv4,omitempty"`
	IPv6     string `json:"ipv6,omitempty"`
}

// ACLDERPRegionDoc duplicates relevant fields from tsclient.ACLDERPRegion
// so Swag doesn't try to parse the external package.
type ACLDERPRegionDoc struct {
	RegionID   int              `json:"regionID,omitempty"`
	RegionCode string           `json:"regionCode,omitempty"`
	RegionName string           `json:"regionName,omitempty"`
	Nodes      []ACLDERPNodeDoc `json:"nodes,omitempty"`
}

// ACLDERPMapDoc duplicates tsclient.ACLDERPMap for Swag docs.
// The real type has Regions map[int]*ACLDERPRegion, so we do a doc-friendly version.
type ACLDERPMapDoc struct {
	OmitDefaultRegions bool                          `json:"omitDefaultRegions,omitempty"`
	Regions            map[int]ACLDERPRegionDoc      `json:"regions,omitempty"`
}

// RegisterRoutes wires up the /derpmap endpoints.
// We'll store the config in state.Data["derpMap"] as a single object.
//
//   GET    /derpmap => retrieve the entire ACLDERPMap
//   POST   /derpmap => create a new DERPMap if none exists
//   PUT    /derpmap => update if exists
//   DELETE /derpmap => remove from state
func RegisterRoutes(r *gin.Engine, state *common.State) {
	d := r.Group("/derpmap")
	{
		d.GET("", func(c *gin.Context) {
			getDERPMap(c, state)
		})
		d.POST("", func(c *gin.Context) {
			createDERPMap(c, state)
		})
		d.PUT("", func(c *gin.Context) {
			updateDERPMap(c, state)
		})
		d.DELETE("", func(c *gin.Context) {
			deleteDERPMap(c, state)
		})
	}
}

// getDERPMap => GET /derpmap
// @Summary      Get DERP map
// @Description  Returns the entire ACLDERPMap if it exists, else returns 404.
// @Tags         DERPMap
// @Accept       json
// @Produce      json
// @Success      200 {object} ACLDERPMapDoc "DERP map found (or empty)"
// @Failure      500 {object} ErrorResponse  "Failed to parse DERPMap"
// @Router       /derpmap [get]
func getDERPMap(c *gin.Context, state *common.State) {
	dm, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse DERPMap"})
		return
	}
	if dm == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No DERPMap found"})
		return
	}
	c.JSON(http.StatusOK, convertDERPMapToDoc(*dm))
}

// createDERPMap => POST /derpmap
// @Summary      Create a new DERPMap
// @Description  Creates a new DERPMap if none exists. If one already exists, returns 409.
// @Tags         DERPMap
// @Accept       json
// @Produce      json
// @Param        derpMap body ACLDERPMapDoc true "DERPMap data"
// @Success      201 {object} ACLDERPMapDoc
// @Failure      400 {object} ErrorResponse "Invalid JSON body"
// @Failure      409 {object} ErrorResponse "DERPMap already exists"
// @Failure      500 {object} ErrorResponse "Failed to parse or save DERPMap"
// @Router       /derpmap [post]
func createDERPMap(c *gin.Context, state *common.State) {
	var newDMDoc ACLDERPMapDoc

	
	if err := c.ShouldBindJSON(&newDMDoc); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	existing, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to read existing DERPMap"})
		return
	}
	if existing != nil {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "DERPMap already exists"})
		return
	}

	newDM := convertDocToDERPMap(newDMDoc)
	if err := state.UpdateKeyAndSave("derpMap", newDM); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save DERPMap"})
		return
	}
	c.JSON(http.StatusCreated, newDMDoc)
}

// updateDERPMap => PUT /derpmap
// @Summary      Update an existing DERPMap
// @Description  Updates the DERPMap if it exists, or returns 404 if not found.
// @Tags         DERPMap
// @Accept       json
// @Produce      json
// @Param        derpMap body ACLDERPMapDoc true "Updated DERPMap data"
// @Success      200 {object} ACLDERPMapDoc
// @Failure      400 {object} ErrorResponse "Invalid JSON body"
// @Failure      404 {object} ErrorResponse "No DERPMap found to update"
// @Failure      500 {object} ErrorResponse "Failed to update DERPMap"
// @Router       /derpmap [put]
func updateDERPMap(c *gin.Context, state *common.State) {
	var updatedDoc ACLDERPMapDoc
	if err := c.ShouldBindJSON(&updatedDoc); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	existing, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse DERPMap"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No DERPMap found to update"})
		return
	}

	newDM := convertDocToDERPMap(updatedDoc)
	if err := state.UpdateKeyAndSave("derpMap", newDM); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update DERPMap"})
		return
	}
	c.JSON(http.StatusOK, updatedDoc)
}

// deleteDERPMap => DELETE /derpmap
// @Summary      Delete the DERPMap
// @Description  Removes the DERPMap from state. Returns 404 if not found.
// @Tags         DERPMap
// @Accept       json
// @Produce      json
// @Success      200 {object} map[string]string "DERPMap deleted"
// @Failure      404 {object} ErrorResponse "No DERPMap found to delete"
// @Failure      500 {object} ErrorResponse "Failed to delete DERPMap"
// @Router       /derpmap [delete]
func deleteDERPMap(c *gin.Context, state *common.State) {
	existing, err := getDERPMapFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse DERPMap"})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "No DERPMap found to delete"})
		return
	}

	if err := state.UpdateKeyAndSave("derpMap", nil); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete DERPMap"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "DERPMap deleted"})
}

// getDERPMapFromState re-marshals state.Data["derpMap"] into *tsclient.ACLDERPMap
func getDERPMapFromState(state *common.State) (*tsclient.ACLDERPMap, error) {
	raw := state.GetValue("derpMap")
	if raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var dm tsclient.ACLDERPMap
	if err := json.Unmarshal(b, &dm); err != nil {
		return nil, err
	}
	return &dm, nil
}

// convertDERPMapToDoc transforms the real tsclient.ACLDERPMap into ACLDERPMapDoc.
func convertDERPMapToDoc(dm tsclient.ACLDERPMap) ACLDERPMapDoc {
	docRegions := make(map[int]ACLDERPRegionDoc, len(dm.Regions))
	for rID, regionPtr := range dm.Regions {
		if regionPtr == nil {
			docRegions[rID] = ACLDERPRegionDoc{}
			continue
		}
		var docNodes []ACLDERPNodeDoc
		for _, nodePtr := range regionPtr.Nodes {
			if nodePtr == nil {
				continue
			}
			docNodes = append(docNodes, ACLDERPNodeDoc{
				Name:     nodePtr.Name,
				RegionID: nodePtr.RegionID,
				HostName: nodePtr.HostName,
				IPv4:     nodePtr.IPv4,
				IPv6:     nodePtr.IPv6,
			})
		}
		docRegions[rID] = ACLDERPRegionDoc{
			RegionID:   rID, // Reflect the map key
			RegionCode: regionPtr.RegionCode,
			RegionName: regionPtr.RegionName,
			Nodes:      docNodes,
		}
	}
	return ACLDERPMapDoc{
		OmitDefaultRegions: dm.OmitDefaultRegions,
		Regions:            docRegions,
	}
}

// convertDocToDERPMap transforms ACLDERPMapDoc into the real tsclient.ACLDERPMap.
func convertDocToDERPMap(doc ACLDERPMapDoc) tsclient.ACLDERPMap {
    realRegions := make(map[int]*tsclient.ACLDERPRegion, len(doc.Regions))
    for rID, docRegion := range doc.Regions {
        var realNodes []*tsclient.ACLDERPNode
        for _, n := range docRegion.Nodes {
            realNodes = append(realNodes, &tsclient.ACLDERPNode{
                Name:     n.Name,
                RegionID: n.RegionID,
                HostName: n.HostName,
                IPv4:     n.IPv4,
                IPv6:     n.IPv6,
            })
        }

        realRegions[rID] = &tsclient.ACLDERPRegion{
            // This line ensures regionID won't be zero in the Tailscale struct:
            RegionID:   rID,

            RegionCode: docRegion.RegionCode,
            RegionName: docRegion.RegionName,
            Nodes:      realNodes,
        }
    }
    return tsclient.ACLDERPMap{
        OmitDefaultRegions: doc.OmitDefaultRegions,
        Regions:            realRegions,
    }
}

