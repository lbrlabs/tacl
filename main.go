package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/acl/acls"
	"github.com/lbrlabs/tacl/pkg/acl/acltests"
	"github.com/lbrlabs/tacl/pkg/acl/autoapprovers"
	"github.com/lbrlabs/tacl/pkg/acl/derpmap"
	"github.com/lbrlabs/tacl/pkg/acl/groups"
	nodeattrs "github.com/lbrlabs/tacl/pkg/acl/nodeattributes"
	"github.com/lbrlabs/tacl/pkg/acl/settings"
	"github.com/lbrlabs/tacl/pkg/acl/ssh"
	"github.com/lbrlabs/tacl/pkg/common"
	"go.uber.org/zap"

	// NEW imports:
	"golang.org/x/oauth2/clientcredentials"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

func main() {

	// set the required unstable API flag
	tailscale.I_Acknowledge_This_API_Is_Unstable = true

	// Flags
	debug := flag.Bool("debug", false, "Enable debug mode to print resulting JSON to stdout")
	storage := flag.String("storage", "file://state.json", "Storage location (file://path or s3://bucket)")
	clientID := flag.String("client-id", "", "Tailscale OAuth client ID (optional)")
	clientSecret := flag.String("client-secret", "", "Tailscale OAuth client secret (optional)")
	tags := flag.String("tags", "tag:tacl", "Comma-separated tags for ephemeral keys (e.g. 'tag:prod,tag:k8s')")
	ephemeral := flag.Bool("ephemeral", true, "Whether the Tailscale node is ephemeral (no stored identity). If false, a Dir is used.")
	hostname := flag.String("hostname", "tacl", "Tailscale hostname")
	port := flag.Int("port", 8080, "Port to listen on")
	stateDir := flag.String("state-dir", "./tacl-ts-state", "Directory to store Tailscale node state if ephemeral=false")

	flag.Parse()

	// Initialize zap logger
	logger := common.InitializeLogger()
	defer logger.Sync()

	// Initialize our shared state (with RWMutex inside)
	state := &common.State{
		Data:    make(map[string]interface{}),
		Storage: *storage,
		Logger:  logger,
		Debug:   *debug,
	}

	// If using S3, set up the client and bucket
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

	// Attempt to load existing state (from file or S3)
	state.LoadFromStorage()

	// Initialize Gin
	r := gin.Default()

	// Register group routes
	groups.RegisterRoutes(r, state)
	acls.RegisterRoutes(r, state)
	autoapprovers.RegisterRoutes(r, state)
	derpmap.RegisterRoutes(r, state)
	acltests.RegisterRoutes(r, state)
	ssh.RegisterRoutes(r, state)
	settings.RegisterRoutes(r, state)
	nodeattrs.RegisterRoutes(r, state)

	r.GET("/state", func(c *gin.Context) {
		c.String(200, state.ToJSON())
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.String(200, "OK")
	})

	// If --debug was set, log the state after POST/PUT
	if *debug {
		r.Use(func(c *gin.Context) {
			c.Next()
			method := c.Request.Method
			if method == "POST" || method == "PUT" || method == "DELETE" {
				jsonState := state.ToJSON()
				logger.Info("Debug Mode - Current State", zap.String("state", jsonState))
				println("Debug Mode - Current State:\n" + jsonState)
			}
		})
	}

	// NEW: Create a tsnet.Server that listens ONLY on Tailscale
	tsServer := &tsnet.Server{
		Hostname:  *hostname,
		Ephemeral: *ephemeral,
		Logf: func(format string, args ...interface{}) {
			logger.Sugar().Debugf("[tsnet] "+format, args...)
		},
	}
	if !*ephemeral {
		tsServer.Dir = *stateDir
	}

	// Start the Tailscale server in the background
	if err := tsServer.Start(); err != nil {
		logger.Fatal("tsnet server failed to start", zap.Error(err))
	}
	defer tsServer.Close()

	// If user passed --client-id and --client-secret, we try ephemeral key approach
	oidcEnabled := (*clientID != "" && *clientSecret != "")
	if oidcEnabled {
		// Create Tailscale Admin client using OAuth2 client credentials
		creds := clientcredentials.Config{
			ClientID:     *clientID,
			ClientSecret: *clientSecret,
			TokenURL:     "https://login.tailscale.com/api/v2/oauth/token",
		}
		tsAdminClient := tailscale.NewClient("-", nil)
		tsAdminClient.HTTPClient = creds.Client(context.Background())

		lc, err := tsServer.LocalClient()
		if err != nil {
			logger.Fatal("Could not get local client from tsnet server", zap.Error(err))
		}

		// Poll until Tailscale is Running, or create ephemeral keys on NeedsLogin
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

				caps := tailscale.KeyCapabilities{
					Devices: tailscale.KeyDeviceCapabilities{
						Create: tailscale.KeyDeviceCreateCapabilities{
							Reusable:      false,
							Preauthorized: true, // set false if you want admin approval
							Tags:          strings.Split(*tags, ","),
						},
					},
				}
				authKey, _, err := tsAdminClient.CreateKey(ctx, caps)
				if err != nil {
					logger.Fatal("Failed creating ephemeral auth key via Admin API", zap.Error(err))
				}

				// Start tailscaled with ephemeral key
				if err := lc.Start(ctx, ipn.Options{AuthKey: authKey}); err != nil {
					logger.Fatal("Failed to Start Tailscale with ephemeral key", zap.Error(err))
				}

				// If ephemeral is not preauthorized, might need interactive login:
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
				// Just keep waiting
			}
			time.Sleep(1 * time.Second)
		}
		logger.Info("Tailscale node is now Running via OIDC ephemeral login.")
	} else {
		logger.Info("No client-id/secret provided; if Tailscale needs login, check logs for a URL.")
	}

	ln, err := tsServer.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		logger.Fatal("tsnet.Listen failed", zap.Error(err))
	}
	defer ln.Close()

	logger.Info("Starting tacl server on Tailscale network", zap.String("addr", ln.Addr().String()), zap.String("portal", fmt.Sprintf("%d", *port)))
	if err := r.RunListener(ln); err != nil {
		logger.Fatal("Gin server failed on tsnet listener", zap.Error(err))
	}
}
