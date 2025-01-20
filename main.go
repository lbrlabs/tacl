package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
	"go.uber.org/zap"

	"golang.org/x/oauth2/clientcredentials"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

func main() {

	// Acknowledge Tailscale's unstable API
	tailscale.I_Acknowledge_This_API_Is_Unstable = true

	// Flags
	debug := flag.Bool("debug", false, "Print debug logs logs")
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
	logger := common.InitializeLogger(*debug)

	defer logger.Sync()

	if *debug {

	}

	log.SetFlags(0)
	log.SetOutput(common.NewConditionalZapWriter(*debug, logger))

	// Initialize our shared state
	state := &common.State{
		Data:    make(map[string]interface{}),
		Storage: *storage,
		Logger:  logger,
		Debug:   *debug,
	}

	// Setup S3 if needed
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

	// Attempt to load existing state (file or S3)
	state.LoadFromStorage()

	// Create our tsnet.Server (Tailscale in-process)
	tsServer := &tsnet.Server{
		Hostname:  *hostname,
		Ephemeral: *ephemeral,
		// "backend" logs
		Logf: func(format string, args ...interface{}) {
			// these logs are quite noisy, so log them as debug
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

	// Start it
	if err := tsServer.Start(); err != nil {
		logger.Fatal("tsnet server failed to start", zap.Error(err))
	}
	defer tsServer.Close()

	// Initialize Gin engine
	r := gin.New()

	// Enforce Tailscale-based capability checks on *all* routes
	r.Use(cap.TailscaleAuthMiddleware(tsServer, logger))

	// use ginzap logger
	r.Use(ginzap.Ginzap(logger, time.RFC3339, true))

	// Logs all panic to error log
	//   - stack means whether output the stack info.
	r.Use(ginzap.RecoveryWithZap(logger, true))

	// Register your routes AFTER the middleware is applied
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

	// If debug, log state after POST/PUT/DELETE
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

	// If user passed --client-id / --client-secret, we do ephemeral key approach
	oidcEnabled := (*clientID != "" && *clientSecret != "")
	if oidcEnabled {
		// Build Tailscale Admin client
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
							Preauthorized: true, // or false if admin approval is required
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
				// Keep waiting
			}
			time.Sleep(1 * time.Second)
		}
		logger.Info("Tailscale node is now Running via OIDC ephemeral login.")
	} else {
		logger.Info("No client-id/secret provided; if Tailscale needs login, check logs for a URL.")
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
