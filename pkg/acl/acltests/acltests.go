package acltests

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lbrlabs/tacl/pkg/common"
)

// ErrorResponse can be used in @Failure annotations for clearer error messages.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ACLTest defines a test structure for ACL rules.
// @Description ACLTest holds test scenarios like "deny" rules, "accept" rules, protocol, etc.
type ACLTest struct {
	// Deny is a list of rules or addresses to be denied.
	Deny []string `json:"deny,omitempty" hujson:"Deny,omitempty"`
	
	// Source is a string describing the traffic source (e.g., IP or user).
	Source string `json:"src,omitempty" hujson:"Src,omitempty"`

	// Proto indicates the protocol (tcp, udp, etc.).
	Proto string `json:"proto,omitempty" hujson:"Proto,omitempty"`

	// Accept is a list of rules or addresses to be accepted.
	Accept []string `json:"accept,omitempty" hujson:"Accept,omitempty"`
}

// ExtendedACLTest represents one test item with a stable UUID-based ID.
// @Description ExtendedACLTest includes the test data plus an auto-generated ID.
type ExtendedACLTest struct {
	ID string `json:"id"`
	ACLTest
}

// updateTestRequest is the body shape for PUT /acltests.
//
// Example JSON:
//  {
//    "id": "some-uuid",
//    "test": {
//      "deny": ["example"],
//      "src": "10.0.0.1",
//      "proto": "tcp",
//      "accept": ["example-accept"]
//    }
//  }
type updateTestRequest struct {
	ID   string  `json:"id"`
	Test ACLTest `json:"test"`
}

// deleteTestRequest is the body shape for DELETE /acltests.
//
// Example JSON:
//  { "id": "some-uuid" }
type deleteTestRequest struct {
	ID string `json:"id"`
}

// RegisterRoutes wires up the ACLTest-related routes at /acltests:
//
//   GET    /acltests      => list all ExtendedACLTests
//   GET    /acltests/:id  => get one by ID
//   POST   /acltests      => create a new test (generates UUID)
//   PUT    /acltests      => update an existing test by ID
//   DELETE /acltests      => delete by ID
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
// @Summary      List all ACL tests
// @Description  Returns all ExtendedACLTest items from storage.
// @Tags         ACLTests
// @Accept       json
// @Produce      json
// @Success      200 {array}  ExtendedACLTest "List of ACL test items"
// @Failure      500 {object} ErrorResponse   "Failed to parse ACLTests"
// @Router       /acltests [get]
func listACLTests(c *gin.Context, state *common.State) {
	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLTests"})
		return
	}
	c.JSON(http.StatusOK, tests)
}

// getACLTestByID => GET /acltests/:id => find by stable UUID
// @Summary      Get one ACL test by ID
// @Description  Retrieves an ACL test item by its stable UUID.
// @Tags         ACLTests
// @Accept       json
// @Produce      json
// @Param        id   path      string true "ACLTest ID"
// @Success      200  {object}  ExtendedACLTest
// @Failure      404  {object}  ErrorResponse "ACLTest not found with that ID"
// @Failure      500  {object}  ErrorResponse "Failed to parse ACLTests"
// @Router       /acltests/{id} [get]
func getACLTestByID(c *gin.Context, state *common.State) {
	id := c.Param("id")

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLTests"})
		return
	}

	for _, test := range tests {
		if test.ID == id {
			c.JSON(http.StatusOK, test)
			return
		}
	}
	c.JSON(http.StatusNotFound, ErrorResponse{Error: "ACLTest not found with that ID"})
}

// createACLTest => POST /acltests
// @Summary      Create a new ACL test
// @Description  Creates a new test item with a generated UUID, storing the provided ACLTest fields.
// @Tags         ACLTests
// @Accept       json
// @Produce      json
// @Param        test  body      ACLTest true "ACLTest fields"
// @Success      201   {object}  ExtendedACLTest
// @Failure      400   {object}  ErrorResponse "Bad request"
// @Failure      500   {object}  ErrorResponse "Failed to parse or save ACLTests"
// @Router       /acltests [post]
func createACLTest(c *gin.Context, state *common.State) {
	var newData ACLTest
	if err := c.ShouldBindJSON(&newData); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLTests"})
		return
	}

	newTest := ExtendedACLTest{
		ID:      uuid.NewString(),
		ACLTest: newData,
	}

	tests = append(tests, newTest)
	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save new ACLTest"})
		return
	}
	c.JSON(http.StatusCreated, newTest)
}

// updateACLTest => PUT /acltests
// @Summary      Update an ACL test
// @Description  Updates an existing ACL test by ID with new ACLTest fields.
// @Tags         ACLTests
// @Accept       json
// @Produce      json
// @Param        body  body      updateTestRequest true "Update ACLTest request"
// @Success      200   {object}  ExtendedACLTest
// @Failure      400   {object}  ErrorResponse "Missing or invalid request data"
// @Failure      404   {object}  ErrorResponse "ACLTest not found with that ID"
// @Failure      500   {object}  ErrorResponse "Failed to update ACLTest"
// @Router       /acltests [put]
func updateACLTest(c *gin.Context, state *common.State) {
	var req updateTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing ACLTest 'id' in request body"})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLTests"})
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
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "ACLTest not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("aclTests", tests); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to update ACLTest"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// deleteACLTest => DELETE /acltests
// @Summary      Delete an ACL test
// @Description  Deletes an ACLTest by specifying its ID in the request body.
// @Tags         ACLTests
// @Accept       json
// @Produce      json
// @Param        body  body      deleteTestRequest true "Delete ACLTest request"
// @Success      200   {object}  map[string]string "ACLTest deleted"
// @Failure      400   {object}  ErrorResponse "Missing or invalid ID"
// @Failure      404   {object}  ErrorResponse "ACLTest not found with that ID"
// @Failure      500   {object}  ErrorResponse "Failed to delete ACLTest"
// @Router       /acltests [delete]
func deleteACLTest(c *gin.Context, state *common.State) {
	var req deleteTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.ID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing 'id' field"})
		return
	}

	tests, err := getACLTestsFromState(state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to parse ACLTests"})
		return
	}

	newList := make([]ExtendedACLTest, 0, len(tests))
	deleted := false
	for _, t := range tests {
		if t.ID == req.ID {
			deleted = true
			continue // skip => remove
		}
		newList = append(newList, t)
	}
	if !deleted {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "ACLTest not found with that ID"})
		return
	}

	if err := state.UpdateKeyAndSave("aclTests", newList); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to delete ACLTest"})
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
