package acltests

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2"
)

// ExtendedACLTest represents one test item with a stable UUID-based ID.
type ExtendedACLTest struct {
	ID string `json:"id"`
	tsclient.ACLTest
}

// RegisterRoutes wires up the ACLTest-related routes at /acltests, now ID-based.
//
//  GET    /acltests       => list all ExtendedACLTests
//  GET    /acltests/:id   => get one by ID
//  POST   /acltests       => create a new test (generates UUID)
//  PUT    /acltests       => update an existing test by ID in JSON
//  DELETE /acltests       => delete by ID in JSON
func RegisterRoutes(r *gin.Engine, state *common.State) {
	t := r.Group("/acltests")
	{
		t.GET("", func(c *gin.Context) {
			listACLTests(c, state)
		})

		t.GET("/:id", func(c *gin.Context) {
			getACLTestByID(c, state)
		})

		t.POST("", func(c *gin.Context) {
			createACLTest(c, state)
		})

		t.PUT("", func(c *gin.Context) {
			updateACLTest(c, state)
		})

		t.DELETE("", func(c *gin.Context) {
			deleteACLTest(c, state)
		})
	}
}

// listACLTests => GET /acltests => returns entire []ExtendedACLTest
func listACLTests(c *gin.Context, state *common.State) {
	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}
	c.JSON(http.StatusOK, tests)
}

// getACLTestByID => GET /acltests/:id => find by stable UUID
func getACLTestByID(c *gin.Context, state *common.State) {
	id := c.Param("id")

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	for _, test := range tests {
		if test.ID == id {
			c.JSON(http.StatusOK, test)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "ACLTest not found with that ID"})
}

// createACLTest => POST /acltests
// Body => Tailscale's ACLTest fields. We'll embed them in ExtendedACLTest with a new ID.
func createACLTest(c *gin.Context, state *common.State) {
	var newData tsclient.ACLTest
	if err := c.ShouldBindJSON(&newData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	newTest := ExtendedACLTest{
		ID:       uuid.NewString(), // stable random UUID
		ACLTest:  newData,
	}

	tests = append(tests, newTest)
	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new ACLTest"})
		return
	}
	c.JSON(http.StatusCreated, newTest)
}

// updateACLTest => PUT /acltests => body shape:
//  {
//    "id": "...",
//    "test": {
//      "User": "...",
//      "Accept": true/false,
//      ...
//    }
//  }
func updateACLTest(c *gin.Context, state *common.State) {
	type updateRequest struct {
		ID   string            `json:"id"`
		Test tsclient.ACLTest  `json:"test"`
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing ACLTest 'id' in request body"})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	var updated *ExtendedACLTest
	for i := range tests {
		if tests[i].ID == req.ID {
			// Found => update the embedded ACLTest
			tests[i].ACLTest = req.Test
			updated = &tests[i]
			break
		}
	}
	if updated == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACLTest not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update ACLTest"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteACLTest => DELETE /acltests => body => { "id": "<uuid>" }
func deleteACLTest(c *gin.Context, state *common.State) {
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

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	newList := make([]ExtendedACLTest, 0, len(tests))
	deleted := false
	for _, t := range tests {
		if t.ID == req.ID {
			// skip it => effectively remove
			deleted = true
			continue
		}
		newList = append(newList, t)
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACLTest not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("aclTests", newList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete ACLTest"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ACLTest deleted"})
}

// getACLTestsFromState => read state.Data["aclTests"] => []ExtendedACLTest
func getACLTestsFromState(state *common.State) ([]ExtendedACLTest, error) {
	raw := state.GetValue("aclTests")
	if raw == nil {
		// If no data has been set yet, return an empty slice
		return []ExtendedACLTest{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var tests []ExtendedACLTest
	if err := json.Unmarshal(b, &tests); err != nil {
		return nil, err
	}
	return tests, nil
}
