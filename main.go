package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/acl/acls"
	"github.com/lbrlabs/tacl/pkg/acl/acltests"
	"github.com/lbrlabs/tacl/pkg/acl/autoapprovers"
	"github.com/lbrlabs/tacl/pkg/acl/derpmap"
	"github.com/lbrlabs/tacl/pkg/acl/groups"
	"github.com/lbrlabs/tacl/pkg/acl/hosts"
	nodeattrs "github.com/lbrlabs/tacl/pkg/acl/nodeattributes"
	"github.com/lbrlabs/tacl/pkg/acl/postures"
	"github.com/lbrlabs/tacl/pkg/acl/settings"
	"github.com/lbrlabs/tacl/pkg/acl/ssh"
	"github.com/lbrlabs/tacl/pkg/cap"
	"github.com/lbrlabs/tacl/pkg/common"
	"github.com/lbrlabs/tacl/pkg/sync"
	"go.uber.org/zap"

	"golang.org/x/oauth2/clientcredentials"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

var (
	// Version is the current version of the application.
	Version = "dev"
)

func main() {
	tailscale.I_Acknowledge_This_API_Is_Unstable = true

	// Flags
	debug := flag.Bool("debug", false, "Print debug logs")
	storage := flag.String("storage", "file://state.json", "Storage location (file://path or s3://bucket)")
	clientID := flag.String("client-id", "", "Tailscale OAuth client ID (optional)")
	clientSecret := flag.String("client-secret", "", "Tailscale OAuth client secret (optional)")
	tags := flag.String("tags", "tag:tacl", "Comma-separated tags for ephemeral keys (e.g. 'tag:prod,tag:k8s')")
	ephemeral := flag.Bool("ephemeral", true, "Whether the Tailscale node is ephemeral (no stored identity). If false, a Dir is used.")
	hostname := flag.String("hostname", "tacl", "Tailscale hostname")
	port := flag.Int("port", 8080, "Port to listen on")
	stateDir := flag.String("state-dir", "./tacl-ts-state", "Directory to store Tailscale node state if ephemeral=false")

	tailnetName := flag.String("tailnet", "", "Your Tailscale tailnet name, e.g. 'mycorp.com'")
	syncInterval := flag.Duration("sync-interval", 30*time.Second, "How often to push ACL state to Tailscale")
	version := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	if *version {
		fmt.Println("Version:", Version)
		return
	}

	// Initialize zap logger
	logger := common.InitializeLogger(*debug)
	defer logger.Sync()

	// Setup standard library -> Zap
	log.SetFlags(0)
	log.SetOutput(common.NewConditionalZapWriter(*debug, logger))

	// Initialize our shared state
	state := &common.State{
		Data:    make(map[string]interface{}),
		Storage: *storage,
		Logger:  logger,
		Debug:   *debug,
	}

	// Possibly set up S3
	if strings.HasPrefix(*storage, "s3://") {
		s3Client, bucket, err := common.InitializeS3Client(*storage)
		if err != nil {
			logger.Fatal("Failed to initialize S3 storage", zap.Error(err))
		}
		state.S3Client = s3Client
		state.Bucket = bucket
	} else if !strings.HasPrefix(*storage, "file://") {
		logger.Fatal("Invalid storage scheme. Must be file:// or s3://")
	}

	// Load existing state from file or S3
	state.LoadFromStorage()

	// Create tsnet server
	tsServer := &tsnet.Server{
		Hostname:  *hostname,
		Ephemeral: *ephemeral,
		Logf: func(format string, args ...interface{}) {
			logger.With(zap.String("component", "tsnet"), zap.String("tsnet_log_source", "backend")).
				Sugar().
				Debugf(format, args...)
		},
		UserLogf: func(format string, args ...interface{}) {
			logger.With(zap.String("component", "tsnet"), zap.String("tsnet_log_source", "user")).
				Sugar().
				Infof(format, args...)
		},
	}
	if !*ephemeral {
		tsServer.Dir = *stateDir
	}

	if err := tsServer.Start(); err != nil {
		logger.Fatal("tsnet server failed to start", zap.Error(err))
	}
	defer tsServer.Close()

	// Build the Gin engine
	r := gin.New()

	// Add Tailscale-based capabilities middleware
	r.Use(cap.TailscaleAuthMiddleware(tsServer, logger))

	// gin zap logger
	r.Use(ginzap.Ginzap(logger, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(logger, true))

	// Register routes
	groups.RegisterRoutes(r, state)
	acls.RegisterRoutes(r, state)
	autoapprovers.RegisterRoutes(r, state)
	derpmap.RegisterRoutes(r, state)
	acltests.RegisterRoutes(r, state)
	ssh.RegisterRoutes(r, state)
	settings.RegisterRoutes(r, state)
	nodeattrs.RegisterRoutes(r, state)
	hosts.RegisterRoutes(r, state)
	postures.RegisterRoutes(r, state)

	// Some basic endpoints
	r.GET("/state", func(c *gin.Context) {
		c.String(200, state.ToJSON())
	})
	r.GET("/healthz", func(c *gin.Context) {
		c.String(200, "OK")
	})

	if *debug {
		// Additional debug info after POST/PUT/DELETE
		r.Use(func(c *gin.Context) {
			c.Next()
			method := c.Request.Method
			if method == "POST" || method == "PUT" || method == "DELETE" {
				jsonState := state.ToJSON()
				logger.Info("Debug Mode - Current State", zap.String("state", jsonState))
				fmt.Println("Debug Mode - Current State:\n" + jsonState)
			}
		})
	}

	// If user provided client-id, client-secret, do ephemeral key approach
	oidcEnabled := (*clientID != "" && *clientSecret != "")
	var adminClient *tailscale.Client // We'll use this to create an API key

	if oidcEnabled {
		// Build Tailscale Admin client using OAuth2
		creds := clientcredentials.Config{
			ClientID:     *clientID,
			ClientSecret: *clientSecret,
			TokenURL:     "https://login.tailscale.com/api/v2/oauth/token",
		}
		adminClient = tailscale.NewClient("-", nil)
		adminClient.HTTPClient = creds.Client(context.Background())

		lc, err := tsServer.LocalClient()
		if err != nil {
			logger.Fatal("Could not get local client from tsnet server", zap.Error(err))
		}

		// Wait until Tailscale is Running or do ephemeral keys if needed
		ctx := context.Background()
		loginDone := false
		machineAuthShown := false

	waitOnline:
		for {
			st, err := lc.StatusWithoutPeers(ctx)
			if err != nil {
				logger.Fatal("Error getting Tailscale status", zap.Error(err))
			}
			switch st.BackendState {
			case "Running":
				break waitOnline

			case "NeedsLogin":
				if loginDone {
					break
				}
				logger.Info("Tailscale NeedsLogin -> creating ephemeral auth key via OIDC")

				keyCaps := tailscale.KeyCapabilities{
					Devices: tailscale.KeyDeviceCapabilities{
						Create: tailscale.KeyDeviceCreateCapabilities{
							Reusable:      false,
							Preauthorized: true,
							Tags:          strings.Split(*tags, ","),
						},
					},
				}
				authKey, _, err := adminClient.CreateKey(ctx, keyCaps)
				if err != nil {
					logger.Fatal("Failed creating ephemeral auth key via Admin API", zap.Error(err))
				}

				// Start tailscaled with ephemeral key
				if err := lc.Start(ctx, ipn.Options{AuthKey: authKey}); err != nil {
					logger.Fatal("Failed to Start Tailscale with ephemeral key", zap.Error(err))
				}

				if err := lc.StartLoginInteractive(ctx); err != nil {
					logger.Fatal("Failed StartLoginInteractive", zap.Error(err))
				}
				loginDone = true

			case "NeedsMachineAuth":
				if !machineAuthShown {
					logger.Info("Machine approval required; visit the Tailscale admin panel to approve.")
					machineAuthShown = true
				}

			default:
				// keep waiting
			}
			time.Sleep(1 * time.Second)
		}
		logger.Info("Tailscale node is now Running via OIDC ephemeral login.")
	} else {
		logger.Info("No client-id/secret provided; if Tailscale needs login, check logs for a URL.")
	}

	// If we have adminClient + tailnetName, let's create an API key & start ACL sync
	if adminClient != nil && *tailnetName != "" {
		// Start ACL sync
		// direct usage of the adminClient to push ACL
		sync.Start(state, adminClient, *tailnetName, *syncInterval)

	} else {
		logger.Warn("Skipping ACL sync: either no tailnet provided or no OAuth2 admin client.")
	}

	// Listen on Tailscale interface
	ln, err := tsServer.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		logger.Fatal("tsnet.Listen failed", zap.Error(err))
	}
	defer ln.Close()

	logger.Info("Starting tacl server on Tailscale network",
		zap.String("addr", ln.Addr().String()),
		zap.Int("port", *port),
	)

	if err := r.RunListener(ln); err != nil {
		logger.Fatal("Gin server failed on tsnet listener", zap.Error(err))
	}
}

// apiKeyTransport is a simple RoundTripper that sets Authorization: Bearer <apiKey>
// in each request.
type apiKeyTransport struct {
	base   http.RoundTripper
	apiKey string
}

func (t *apiKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.apiKey)

	rt := t.base
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(clone)
}
