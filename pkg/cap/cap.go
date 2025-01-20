package cap

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"tailscale.com/tsnet"
)

// TACLManagerCapability is our sub-capability shape:
//
//	"manager": { "methods": [...], "endpoints": [...] }
//
// If "methods" is ["*"], it means all methods are allowed.
// If "endpoints" is ["*"], it means all endpoints are allowed.
type TACLManagerCapability struct {
	Methods   []string `json:"methods"`
	Endpoints []string `json:"endpoints"`
}

// TACLAppCapabilities represents the JSON shape in "lbrlabs.com/cap/tacl", e.g.:
//
//	[
//	  {
//	    "manager": { "methods": [...], "endpoints": [...] }
//	  }
//	]
type TACLAppCapabilities []map[string]TACLManagerCapability

// TailscaleAuthMiddleware enforces that incoming requests have
// the "lbrlabs.com/cap/tacl" -> "manager" capability with the
// correct Method + Endpoint. Otherwise, we return JSON with a
// "permission denied" error message.
func TailscaleAuthMiddleware(tsServer *tsnet.Server, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			logger.Warn("Failed to parse IP from RemoteAddr", zap.String("RemoteAddr", c.Request.RemoteAddr))
			abortWithJSON(c, http.StatusUnauthorized, "permission denied, cannot parse caller IP")
			return
		}

		lc, err := tsServer.LocalClient()
		if err != nil {
			logger.Error("Could not get LocalClient from tsServer", zap.Error(err))
			abortWithJSON(c, http.StatusInternalServerError, "internal error: cannot connect to Tailscale local client")
			return
		}

		st, err := lc.WhoIs(context.Background(), ip)
		if err != nil {
			logger.Warn("WhoIs lookup failed", zap.String("ip", ip), zap.Error(err))
			abortWithJSON(c, http.StatusUnauthorized, "permission denied, whois lookup failed")
			return
		}

		// Log who Tailscale says is calling us
		userLoginName := ""
		displayName := ""
		if st.UserProfile != nil {
			userLoginName = st.UserProfile.LoginName
			displayName = st.UserProfile.DisplayName
		}
		logger.Info("Incoming request from Tailscale",
			zap.String("ip", ip),
			zap.String("userLoginName", userLoginName),
			zap.String("displayName", displayName),
			zap.String("method", c.Request.Method),
			zap.String("url", c.Request.URL.Path),
		)

		// We expect "lbrlabs.com/cap/tacl"
		rawCap, ok := st.CapMap["lbrlabs.com/cap/tacl"]
		if !ok {
			logger.Warn("Missing lbrlabs.com/cap/tacl capability",
				zap.String("ip", ip),
				zap.String("userLoginName", userLoginName),
			)
			abortWithJSON(c, http.StatusUnauthorized, "permission denied, please check tailscale capabilities")
			return
		}

		// Re-marshal to JSON
		capBytes, err := json.Marshal(rawCap)
		if err != nil {
			logger.Warn("Failed to marshal raw capability data", zap.Error(err))
			abortWithJSON(c, http.StatusUnauthorized, "permission denied, bad capability data")
			return
		}

		// Unmarshal into our known struct
		var appCaps TACLAppCapabilities
		if err := json.Unmarshal(capBytes, &appCaps); err != nil {
			logger.Warn("Failed to unmarshal TACL capabilities JSON", zap.Error(err))
			abortWithJSON(c, http.StatusUnauthorized, "permission denied, capabilities parse error")
			return
		}

		// Check for manager sub-cap
		method := c.Request.Method
		endpointFirstSegment := firstPathSegment(c.Request.URL.Path)
		allowed := false

		for _, subcapMap := range appCaps {
			if managerCap, haveManager := subcapMap["manager"]; haveManager {
				// If managerCap.Methods includes "*", all methods are allowed.
				// If managerCap.Endpoints includes "*", all endpoints are allowed.
				if matchStringListOrWildcard(method, managerCap.Methods) &&
					matchStringListOrWildcard(endpointFirstSegment, managerCap.Endpoints) {
					allowed = true
					break
				}
			}
		}

		if !allowed {
			logger.Warn("Not authorized by TACL 'manager' capability",
				zap.String("ip", ip),
				zap.String("userLoginName", userLoginName),
				zap.String("method", method),
				zap.String("endpoint", endpointFirstSegment),
			)
			abortWithJSON(c, http.StatusUnauthorized, "permission denied, please check tailscale capabilities")
			return
		}

		// Success!
		c.Next()
	}
}

// matchStringListOrWildcard returns true if `list` has "*"
// or if `item` is in `list`.
func matchStringListOrWildcard(item string, list []string) bool {
	// If the list includes "*", it means "any".
	for _, s := range list {
		if s == "*" {
			return true
		}
	}
	// Otherwise, check membership
	return stringInSlice(item, list)
}

// abortWithJSON aborts the current request with a given status code and JSON error message.
func abortWithJSON(c *gin.Context, code int, message string) {
	c.AbortWithStatusJSON(code, gin.H{"error": message})
}

func stringInSlice(needle string, haystack []string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func firstPathSegment(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "" // e.g. root "/"
	}
	parts := strings.Split(path, "/")
	return parts[0]
}
