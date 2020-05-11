package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"

	"github.com/cloudworkz/grafana-permission-sync/pkg/watcher"
	"github.com/gin-gonic/gin"
)

var (
	config    *Config // current
	newConfig *Config // new config to use next

	log *zap.SugaredLogger

	configPath string

	dryRunNoPlanNoExec = len(os.Getenv("GRAFANA_PERMISSION_SYNC_NO_PLAN_NO_EXEC")) > 0
	dryRunNoExec       = len(os.Getenv("GRAFANA_PERMISSION_SYNC_NO_EXEC")) > 0
)

func main() {
	// 1. Setup, load config, ...
	logRaw, _ := zap.NewProduction()
	log = logRaw.Sugar()

	flag.StringVar(&configPath, "configPath", "./config.yaml", "alternative path to the config file")
	flag.Parse()

	// Load config
	config = tryLoadConfig(configPath)
	if config == nil {
		log.Fatal("can't start, error loading config. initial config must be valid (hot reloaded config may be invalid, in which case the old/previous config will be kept)")
	}

	// Configure hot-reload
	setupConfigHotReload(configPath)

	// 2. Start sync
	setupSync()
	go startSync()

	// 3. Start HTTP server
	startWebServer()
}

func startWebServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	rawLog := log.Desugar()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: func(param gin.LogFormatterParams) string {
			// don't log liveness checks
			if strings.HasPrefix(param.Path, "/admin/") {
				return ""
			}

			fields := []zapcore.Field{
				zap.String("clientIP", param.ClientIP),
				zap.String("method", param.Method),
				zap.String("path", param.Path),
				zap.String("protocol", param.Request.Proto),
				zap.Int("statusCode", param.StatusCode),
				zap.Duration("latency", param.Latency),
				zap.String("userAgent", param.Request.UserAgent()),
			}

			if param.ErrorMessage != "" {
				fields = append(fields, zap.String("error", param.ErrorMessage))
			}

			rawLog.Info("handling http request", fields...)
			return "" // prevent unstructured logging
		},
	}))

	r.Use(gin.Recovery())

	r.GET("/admin/ready", func(c *gin.Context) {
		if createdPlans > 0 {
			renderYAML(c, 200, gin.H{"status": "ready"})
		} else {
			renderYAML(c, 503, gin.H{"status": "starting"})
		}
	})

	r.GET("/admin/alive", func(c *gin.Context) {
		renderYAML(c, 200, gin.H{"status": "ready"})
	})

	r.GET("/admin/groups/:email", func(c *gin.Context) {
		email := c.Param("email")
		recurse := c.Query("recurse") == "true"
		members, err := groupTree.ListGroupMembersForDisplay(email, recurse)
		if err != nil {
			renderJSON(c, 500, gin.H{"error": err.Error()})
			return
		}
		renderJSON(c, 200, members)
	})

	r.GET("/admin/users/:email", func(c *gin.Context) {
		email := c.Param("email")
		groups, err := groupTree.ListUserGroupsForDisplay(email)
		if err != nil {
			renderJSON(c, 500, gin.H{"error": err.Error()})
			return
		}
		renderJSON(c, 200, groups)
		// todo:
		// 1. resulting in permissions: x, y, z, ...
		// 2. and the user would not be in organizations: a, b, c, ...
	})

	err := r.Run(":3000")
	if err != nil {
		log.Fatalw("error in router.Run", "error", err)
	}
}

func renderYAML(c *gin.Context, code int, obj interface{}) {
	bytes, err := yaml.Marshal(obj)
	if err != nil {
		c.String(500, "error: "+err.Error())
	}
	c.Data(code, "text/plain; charset=utf-8", bytes)
}

func renderJSON(c *gin.Context, code int, obj interface{}) {
	bytes, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		c.String(500, "error: "+err.Error())
	}
	c.Data(code, "text/plain; charset=utf-8", bytes)
}

func setupConfigHotReload(configPath string) {

	configPathAbs, err := filepath.Abs(configPath)
	if err != nil {
		log.Fatal("cannot build absolute path", "path", configPath, "error", err)
	}

	watcher, err := watcher.WatchPath(configPath)
	if err != nil {
		log.Errorw("can't start config file watcher. config hot-reloading will be disabled!", "error", err)
	}
	watcher.OnError = func(err error) {
		log.Errorw("error in config watcher", "error", err)
	}
	watcher.OnChange = func(filePath string) {
		filePathAbs, err := filepath.Abs(filePath)
		if err != nil {
			log.Fatal("cannot build absolute path", "path", filePath, "error", err)
		}
		if filePathAbs != configPathAbs {
			log.Warnw("config file watcher notified us about a file we don't want to know about", "fileWeWantToWatch", configPath, "fileReportedByWatcher", filePath)
			return
		}

		// Try to reload the config, and if it is valid, set it
		c := tryLoadConfig(configPath)
		if c == nil {
			log.Error("Config file changed, but loading failed. Will continue with already loaded config and ignore new config.")
			return
		}
		newConfig = c

		log.Info("new config loaded successfully, swapping on next idle phase")

	}
}
