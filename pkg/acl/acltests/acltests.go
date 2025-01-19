package acltests

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/common"
	tsclient "github.com/tailscale/tailscale-client-go/v2" // for ACLTest if it's defined there
)

// RegisterRoutes wires up the ACLTest-related routes at /acltests.
//
// e.g.
//
//	GET    /acltests         => list all tests
//	GET    /acltests/:index  => get one by numeric index
//	POST   /acltests         => create new test
//	PUT    /acltests         => update an existing test by index in JSON
//	DELETE /acltests         => delete a test by index in JSON
func RegisterRoutes(r *gin.Engine, state *common.State) {
	t := r.Group("/acltests")
	{
		t.GET("", func(c *gin.Context) {
			listACLTests(c, state)
		})

		t.GET("/:index", func(c *gin.Context) {
			getACLTestByIndex(c, state)
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

// listACLTests => GET /acltests
func listACLTests(c *gin.Context, state *common.State) {
	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}
	c.JSON(http.StatusOK, tests)
}

// getACLTestByIndex => GET /acltests/:index
func getACLTestByIndex(c *gin.Context, state *common.State) {
	indexStr := c.Param("index")
	i, err := strconv.Atoi(indexStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid index"})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	if i < 0 || i >= len(tests) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACLTest index out of range"})
		return
	}
	c.JSON(http.StatusOK, tests[i])
}

// createACLTest => POST /acltests
func createACLTest(c *gin.Context, state *common.State) {
	var newTest tsclient.ACLTest
	if err := c.ShouldBindJSON(&newTest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	tests = append(tests, newTest)
	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save new ACLTest"})
		return
	}
	c.JSON(http.StatusCreated, newTest)
}

// updateACLTest => PUT /acltests
// User passes JSON with "index" plus the new fields to replace that test.
func updateACLTest(c *gin.Context, state *common.State) {
	type updateRequest struct {
		Index int              `json:"index"`
		Test  tsclient.ACLTest `json:"test"`
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

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	if req.Index >= len(tests) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACLTest index out of range"})
		return
	}

	tests[req.Index] = req.Test

	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update ACLTest"})
		return
	}
	c.JSON(http.StatusOK, req.Test)
}

// deleteACLTest => DELETE /acltests
// The user passes JSON with "index" to remove from the slice.
func deleteACLTest(c *gin.Context, state *common.State) {
	type deleteRequest struct {
		Index int `json:"index"`
	}
	var req deleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse ACLTests"})
		return
	}

	if req.Index < 0 || req.Index >= len(tests) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ACLTest index out of range"})
		return
	}

	tests = append(tests[:req.Index], tests[req.Index+1:]...)
	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete ACLTest"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ACLTest deleted"})
}

// getACLTestsFromState => re-marshal state.Data["aclTests"] into []tsclient.ACLTest
func getACLTestsFromState(state *common.State) ([]tsclient.ACLTest, error) {
	raw := state.GetValue("aclTests") // uses RLock internally
	if raw == nil {
		return []tsclient.ACLTest{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var tests []tsclient.ACLTest
	if err := json.Unmarshal(b, &tests); err != nil {
		return nil, err
	}
	return tests, nil
}
