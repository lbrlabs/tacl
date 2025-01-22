package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"io"
	"net/http"
)

// Equality helper
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Convert []types.String => []string
func toGoStringSlice(tf []types.String) []string {
	out := make([]string, len(tf))
	for i, s := range tf {
		out[i] = s.ValueString()
	}
	return out
}

// Another alias: toStringSlice => same logic
func toStringSlice(arr []types.String) []string {
	out := make([]string, len(arr))
	for i, v := range arr {
		out[i] = v.ValueString()
	}
	return out
}

// Convert []interface{} => []types.String
func toStringTypeSlice(arr []interface{}) []types.String {
	out := make([]types.String, len(arr))
	for i, v := range arr {
		if s, ok := v.(string); ok {
			out[i] = types.StringValue(s)
		} else {
			out[i] = types.StringNull()
		}
	}
	return out
}

// NotFoundError helps identify 404
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}
func IsNotFound(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

// doSingleObjectReq => JSON request for single-object endpoints
func doSingleObjectReq(ctx context.Context, client *http.Client, method, url string, payload interface{}) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewBuffer(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == 404 {
		return nil, &NotFoundError{Message: "object not found"}
	}
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("TACL returned %d: %s", res.StatusCode, msg)
	}

	return io.ReadAll(res.Body)
}

/*
  toStringSliceMap was formerly using attr.ToGoValue(ctx), which is removed in newer versions.

  We'll switch to "ElementsAs" in a hacky way:
    - We know the map's element type is a list of strings => map[string][]types.String
    - We'll decode into that intermediate form.
    - If there's any error, we'll ignore it and return empty map (not ideal, but minimal intrusion).
*/

// toStringSliceMap => used by autoApprovers, etc.
// We keep the same signature: no ctx param, no diag return.
func toStringSliceMap(attr types.Map) map[string][]string {
	if attr.IsNull() || attr.IsUnknown() {
		return make(map[string][]string)
	}

	// We'll decode into map[string][]types.String
	intermediate := make(map[string][]types.String)
	// The framework method => attr.ElementsAs(ctx, &intermediate, false) needs a context + some minimal handling
	diags := attr.ElementsAs(context.Background(), &intermediate, false)
	if diags.HasError() {
		// We'll just return empty if there's a decode failure.
		return make(map[string][]string)
	}

	// Convert []types.String => []string
	out := make(map[string][]string, len(intermediate))
	for k, listOfTFStrings := range intermediate {
		out[k] = toGoStringSlice(listOfTFStrings)
	}
	return out
}

/*
toTerraformMapOfStringList => we build a map[string][]interface{} for plugin framework.
*/
func toTerraformMapOfStringList(m map[string][]string) types.Map {
	if m == nil {
		return types.MapNull(types.ListType{ElemType: types.StringType})
	}
	conv := make(map[string][]interface{}, len(m))
	for k, list := range m {
		tmp := make([]interface{}, len(list))
		for i, s := range list {
			tmp[i] = s
		}
		conv[k] = tmp
	}
	val, diagErr := types.MapValueFrom(
		context.Background(),
		types.ListType{ElemType: types.StringType},
		conv,
	)
	if diagErr != nil {
		// fall back to null map if there's an error
		return types.MapNull(types.ListType{ElemType: types.StringType})
	}
	return val
}

// toTerraformStringSlice => convert []string => []types.String
func toTerraformStringSlice(ss []string) []types.String {
	out := make([]types.String, len(ss))
	for i, s := range ss {
		out[i] = types.StringValue(s)
	}
	return out
}

func isNotFound(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}
