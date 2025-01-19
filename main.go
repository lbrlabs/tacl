package main

import (
	"flag"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lbrlabs/tacl/pkg/acl/acls"
	"github.com/lbrlabs/tacl/pkg/acl/acltests"
	"github.com/lbrlabs/tacl/pkg/acl/autoapprovers"
	"github.com/lbrlabs/tacl/pkg/acl/derpmap"
	"github.com/lbrlabs/tacl/pkg/acl/groups"
	"github.com/lbrlabs/tacl/pkg/acl/hosts"
	nodeattrs "github.com/lbrlabs/tacl/pkg/acl/nodeattributes"
	"github.com/lbrlabs/tacl/pkg/acl/settings"
	"github.com/lbrlabs/tacl/pkg/acl/ssh"
	"github.com/lbrlabs/tacl/pkg/acl/postures"
	"github.com/lbrlabs/tacl/pkg/common"
	"go.uber.org/zap"
)

func main() {
	// Flags
	debug := flag.Bool("debug", false, "Enable debug mode to print resulting JSON to stdout")
	storage := flag.String("storage", "file://state.json", "Storage location (file://path or s3://bucket)")
	flag.Parse()

	// Initialize zap logger
	logger := common.InitializeLogger()
	defer logger.Sync()

	// Initialize our shared state (with RWMutex inside)
	state := &common.State{
		Data:    make(map[string]interface{}),
		Storage: *storage,
		Logger:  logger, // Attach our zap logger
		Debug:   *debug, // Use the --debug flag
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
	hosts.RegisterRoutes(r, state)
	postures.RegisterRoutes(r, state)


	// Optional: debug route to dump the entire state
	r.GET("/state", func(c *gin.Context) {
		// Just return the entire JSON as a string.
		// This uses an RLock internally, so no conflict with writes.
		c.String(200, state.ToJSON())
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

	// Start the server
	port := ":8080"
	logger.Info("Starting server", zap.String("port", port))
	if err := r.Run(port); err != nil {
		logger.Fatal("Server failed to start", zap.Error(err))
	}
}
