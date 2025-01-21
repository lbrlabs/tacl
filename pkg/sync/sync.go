package sync

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/lbrlabs/tacl/pkg/common"
    "go.uber.org/zap"
    "tailscale.com/client/tailscale"
)

// Start sets up a background goroutine that periodically pushes
// local ACL data to Tailscale.
func Start(state *common.State, tsAdminClient *tailscale.Client, tailnetName string, interval time.Duration) {
    if tsAdminClient == nil {
        state.Logger.Warn("tsAdminClient is nil, skipping ACL sync")
        return
    }
    if tailnetName == "" {
        state.Logger.Warn("tailnetName is empty, skipping ACL sync")
        return
    }

    // do one immediate push
    Push(state, tsAdminClient, tailnetName)

    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        for range ticker.C {
            Push(state, tsAdminClient, tailnetName)
        }
    }()
}

// Push => build a Tailscale-friendly JSON, then post it to Tailscale
func Push(state *common.State, tsAdminClient *tailscale.Client, tailnetName string) {
    policyJSON, err := buildTailscaleACLJSON(state)
    if err != nil {
        state.Logger.Error("Failed to build Tailscale ACL JSON", zap.Error(err))
        return
    }
    if policyJSON == "{}" {
        state.Logger.Info("Local state is empty; skipping ACL push.")
        return
    }

    err = putACL(tsAdminClient, tailnetName, []byte(policyJSON))
    if err != nil {
        state.Logger.Error("Failed to push local ACL to Tailscale", zap.Error(err))
        return
    }

    state.Logger.Info("Pushed local ACL to Tailscale",
        zap.Int("bytes", len(policyJSON)))
}

// buildTailscaleACLJSON => deep-clone state.Data, remove "id" fields, return JSON
func buildTailscaleACLJSON(state *common.State) (string, error) {
    state.RWLock.RLock()
    defer state.RWLock.RUnlock()

    // Deep-copy the entire data
    rawBytes, err := json.Marshal(state.Data)
    if err != nil {
        return "", err
    }
    var clone interface{}
    if err := json.Unmarshal(rawBytes, &clone); err != nil {
        return "", err
    }

    // Recursively strip out "id"
    cleaned := removeIDFields(clone)

    // Marshal
    filteredBytes, err := json.MarshalIndent(cleaned, "", "  ")
    if err != nil {
        return "", err
    }
    return string(filteredBytes), nil
}

// removeIDFields => recursively remove "id" from any map
func removeIDFields(obj interface{}) interface{} {
    switch val := obj.(type) {
    case []interface{}:
        // array => recurse
        for i, item := range val {
            val[i] = removeIDFields(item)
        }
        return val
    case map[string]interface{}:
        // remove "id" key
        delete(val, "id")
        // also remove any other custom fields if you want
        for k, v := range val {
            val[k] = removeIDFields(v)
        }
        return val
    default:
        // scalar => return as is
        return obj
    }
}

// putACL => do an HTTP POST to Tailscale's admin API
func putACL(tsAdminClient *tailscale.Client, tailnetName string, aclJSON []byte) error {
    httpClient := tsAdminClient.HTTPClient
    if httpClient == nil {
        return fmt.Errorf("tsAdminClient.HTTPClient is nil; cannot make admin API requests")
    }

    path := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/acl", tailnetName)
    req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, path, bytes.NewReader(aclJSON))
    if err != nil {
        return fmt.Errorf("creating POST request for %s: %w", path, err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST %s failed: %w", path, err)
    }
    defer resp.Body.Close()

    if resp.StatusCode < 200 || resp.StatusCode > 299 {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("POST %s returned %d: %s", path, resp.StatusCode, string(body))
    }
    return nil
}
