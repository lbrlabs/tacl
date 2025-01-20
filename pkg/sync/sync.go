package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lbrlabs/tacl/pkg/common"
	"go.uber.org/zap"
	"tailscale.com/client/tailscale"
)

// StartACLSync starts a background goroutine that periodically pushes
// local ACL data from state["acls"] to Tailscale, overwriting Tailscale's ACL.
func Start(state *common.State, tsAdminClient *tailscale.Client, tailnetName string, interval time.Duration) {
	if tsAdminClient == nil {
		state.Logger.Warn("StartACLSync: tsAdminClient is nil, skipping ACL sync")
		return
	}
	if tailnetName == "" {
		state.Logger.Warn("StartACLSync: tailnetName is empty, skipping ACL sync")
		return
	}

	// Do an immediate push once at startup:
	Push(state, tsAdminClient, tailnetName)

	// Then schedule repeated pushes
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			Push(state, tsAdminClient, tailnetName)
		}
	}()
}

// pushACLFromStateToTailscale overwrites Tailscale's ACL with the local data in state
func Push(state *common.State, tsAdminClient *tailscale.Client, tailnetName string) {

	policyJSON := state.ToJSON()

	if policyJSON == "{}" {
		state.Logger.Info("Local state is empty; skipping ACL push.")
		return
	}

	// 2) call your existing Put(...) function:
	err := Put(tsAdminClient, tailnetName, []byte(policyJSON))
	if err != nil {
		state.Logger.Error("Failed to push local ACL to Tailscale", zap.Error(err))
		return
	}

	state.Logger.Info("Pushed local ACL to Tailscale",
		zap.Int("bytes", len(policyJSON)))
}

// putACLToTailscaleAPI does a real HTTP PUT to Tailscale Admin API with the
// given JSON body. The tsAdminClient is from tailscale.com/client/tailscale.
func Put(tsAdminClient *tailscale.Client, tailnetName string, aclJSON []byte) error {
	httpClient := tsAdminClient.HTTPClient
	if httpClient == nil {
		return fmt.Errorf("tsAdminClient.HTTPClient is nil; cannot make admin API requests")
	}

	path := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/acl", tailnetName)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, path, bytes.NewReader(aclJSON))
	if err != nil {
		return fmt.Errorf("creating PUT request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return nil
}
