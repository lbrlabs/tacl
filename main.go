package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"
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

// Version is the current version of the application.
var Version = "dev"

// CLI defines the flags/environment variables for our command using Kong tags.
type CLI struct {
	Debug bool `help:"Print debug logs" default:"false" env:"TACL_DEBUG"`

	// Storage
	Storage string `help:"Storage location (file://path or s3://bucket[/key])" default:"file://state.json" env:"TACL_STORAGE"`

	// Custom S3 config flags
	S3Endpoint string `help:"Custom S3 endpoint (e.g. minio.local:9000). Defaults to s3.amazonaws.com if not set." env:"TACL_S3_ENDPOINT" name:"s3-endpoint"`
	S3Region   string `help:"AWS or custom S3 region. Defaults to 'us-east-1' if not set." env:"TACL_S3_REGION" name:"s3-region"`

	ClientID     string `help:"Tailscale OAuth client ID" env:"TACL_CLIENT_ID"`
	ClientSecret string `help:"Tailscale OAuth client secret" env:"TACL_CLIENT_SECRET"`

	Tags        string `help:"Comma-separated tags for ephemeral keys (e.g. 'tag:prod,tag:k8s')" default:"tag:tacl" env:"TACL_TAGS"`
	Ephemeral   bool   `help:"Use ephemeral Tailscale node (no stored identity)" default:"true" env:"TACL_EPHEMERAL"`
	Hostname    string `help:"Tailscale hostname" default:"tacl" env:"TACL_HOSTNAME"`
	Port        int    `help:"Port to listen on" default:"8080" env:"TACL_PORT"`
	StateDir    string `help:"Directory to store Tailscale node state if ephemeral=false" default:"./tacl-ts-state" env:"TACL_STATE_DIR"`
	TailnetName string `help:"Your Tailscale tailnet name (e.g. 'mycorp.com')" env:"TACL_TAILNET"`

	SyncInterval time.Duration `help:"How often to push ACL state to Tailscale" default:"30s" env:"TACL_SYNC_INTERVAL"`

	Version bool `help:"Print version and exit" default:"false" env:"TACL_VERSION"`
}

func main() {
	tailscale.I_Acknowledge_This_API_Is_Unstable = true

	// Parse command-line arguments & environment variables via Kong.
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("tacl"),
		kong.Description("A Tailscale-based ACL management server"),
	)
	_ = kctx

	if cli.Version {
		fmt.Println("Version:", Version)
		return
	}

	// Initialize zap logger
	logger := common.InitializeLogger(cli.Debug)
	defer logger.Sync()

	// Setup standard library -> Zap
	log.SetFlags(0)
	log.SetOutput(common.NewConditionalZapWriter(cli.Debug, logger))

	if cli.Debug {
		logger.Debug("Debug mode enabled")
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize shared state
	state := &common.State{
		Data:    make(map[string]interface{}),
		Storage: cli.Storage,
		Logger:  logger,
		Debug:   cli.Debug,
	}

	// Possibly set up S3 if storage is s3://
	if strings.HasPrefix(cli.Storage, "s3://") {
		s3Client, bucket, objectKey, err := common.InitializeS3Client(
			cli.Storage,
			cli.S3Endpoint,
			cli.S3Region,
			logger,
		)
		if err != nil {
			logger.Fatal("Failed to initialize S3 storage", zap.Error(err))
		}
		state.S3Client = s3Client
		state.Bucket = bucket
		state.ObjectKey = objectKey
	} else if !strings.HasPrefix(cli.Storage, "file://") {
		// If not file:// or s3://, bail out
		logger.Fatal("Invalid storage scheme. Must be file:// or s3://")
	}

	// Load existing state from file or S3
	state.LoadFromStorage()

	// Create tsnet server
	tsServer := &tsnet.Server{
		Hostname:  cli.Hostname,
		Ephemeral: cli.Ephemeral,
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

	if !cli.Ephemeral {
		tsServer.Dir = cli.StateDir
	}

	if err := tsServer.Start(); err != nil {
		logger.Fatal("tsnet server failed to start", zap.Error(err))
	}
	defer tsServer.Close()

	// Build the Gin engine
	r := gin.New()

	// remove trusted proxies because we're using Tailscale for auth
	r.SetTrustedProxies(nil)

	// Add Tailscale-based capabilities middleware
	r.Use(cap.TailscaleAuthMiddleware(tsServer, logger))
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
		c.String(http.StatusOK, state.ToJSON())
	})
	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// Optionally print debug info
	if cli.Debug {
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

	//-----------------------------------
	// If user provided client-id & secret, do ephemeral key approach
	oidcEnabled := (cli.ClientID != "" && cli.ClientSecret != "")
	var adminClient *tailscale.Client

	if oidcEnabled {
		// Build Tailscale Admin client using OAuth2
		creds := clientcredentials.Config{
			ClientID:     cli.ClientID,
			ClientSecret: cli.ClientSecret,
			TokenURL:     "https://login.tailscale.com/api/v2/oauth/token",
		}
		adminClient = tailscale.NewClient("-", nil)
		adminClient.HTTPClient = creds.Client(context.Background())

		lc, err := tsServer.LocalClient()
		if err != nil {
			logger.Fatal("Could not get local client from tsnet server", zap.Error(err))
		}

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
							Tags:          strings.Split(cli.Tags, ","),
						},
					},
				}
				authKey, _, err := adminClient.CreateKey(ctx, keyCaps)
				if err != nil {
					logger.Fatal("Failed creating ephemeral auth key via Admin API", zap.Error(err))
				}

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

	//-----------------------------------
	// If we have adminClient + tailnetName, let's create an API key & start ACL sync
	if adminClient != nil && cli.TailnetName != "" {
		sync.Start(state, adminClient, cli.TailnetName, cli.SyncInterval)
	} else {
		logger.Warn("Skipping ACL sync: either no tailnet provided or no OAuth2 admin client.")
	}

	// Listen on Tailscale interface
	ln, err := tsServer.Listen("tcp", fmt.Sprintf(":%d", cli.Port))
	if err != nil {
		logger.Fatal("tsnet.Listen failed", zap.Error(err))
	}
	defer ln.Close()

	logger.Info("Starting tacl server on Tailscale network",
		zap.String("addr", ln.Addr().String()),
		zap.Int("port", cli.Port),
	)

	if err := r.RunListener(ln); err != nil {
		logger.Fatal("Gin server failed on tsnet listener", zap.Error(err))
	}
}
