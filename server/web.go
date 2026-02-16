package server

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	log "github.com/sirupsen/logrus"
)

// WebServer wraps Echo server
type WebServer struct {
	echo   *echo.Echo
	addr   string
	distFS embed.FS
}

// NewWebServer creates and configures the Echo server
func NewWebServer(addr string, distFS embed.FS) *WebServer {

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Middleware
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Add basic auth middleware if web-auth is configured
	if config.WebAuth != "" {
		credentials := parseWebAuthCredentials(config.WebAuth)
		if len(credentials) > 0 {
			e.Use(middleware.BasicAuth(basicAuthValidator(credentials)))
			log.Infof("web: basic authentication enabled for %d user(s)", len(credentials))
		}
	}

	ws := &WebServer{
		echo:   e,
		addr:   addr,
		distFS: distFS,
	}

	ws.setupRoutes()
	return ws
}

// setupRoutes configures all HTTP routes
func (ws *WebServer) setupRoutes() {
	// API routes
	api := ws.echo.Group("/api")
	api.GET("/config", ws.configHandler)
	api.GET("/instances", ws.listInstancesHandler)
	api.GET("/instances/detailed/list", ws.listInstancesDetailedHandler)
	api.GET("/instances/:instance", ws.instanceDetailsHandler)
	api.POST("/instances/:instance/action", ws.instanceActionHandler)
	api.GET("/profiles", ws.profilesHandler)
	api.GET("/images", ws.imagesHandler)
	api.GET("/projects", ws.projectsHandler)
	api.GET("/instances/:instance/exists", ws.instanceExistsHandler)
	api.POST("/instances", ws.createInstanceHandler)

	// WebSocket route
	ws.echo.GET("/ws/ssh/:login", ws.websocketTerminalHandler)

	// Static files and SPA fallback
	ws.setupStaticFiles()
}

// setupStaticFiles serves embedded frontend files
func (ws *WebServer) setupStaticFiles() {
	// Create a sub-filesystem from dist directory
	distSubFS, err := fs.Sub(ws.distFS, "dist")
	if err != nil {
		log.Warnf("failed to create sub filesystem: %v (web UI will not be available until frontend is built)", err)
		// Return early if dist doesn't exist yet (development mode)
		return
	}

	// Serve static files
	staticHandler := http.FileServer(http.FS(distSubFS))

	// Handle all routes for SPA
	ws.echo.GET("/*", echo.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		} else {
			path = path[1:] // Remove leading slash
		}

		if _, err := fs.Stat(distSubFS, path); err == nil {
			staticHandler.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA routing
		r.URL.Path = "/"
		staticHandler.ServeHTTP(w, r)
	})))
}

// Start starts the web server
func (ws *WebServer) Start() error {
	log.Infof("web: starting server on %s", ws.addr)
	return ws.echo.Start(ws.addr)
}

// Shutdown gracefully shuts down the server
func (ws *WebServer) Shutdown(ctx context.Context) error {
	log.Info("web: shutting down server...")
	return ws.echo.Shutdown(ctx)
}

// parseWebAuthCredentials parses comma-separated user:password values
// and returns a map of username to password
func parseWebAuthCredentials(authString string) map[string]string {
	credentials := make(map[string]string)
	if authString == "" {
		return credentials
	}

	pairs := strings.Split(authString, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			log.Warnf("web: invalid credentials format: %s (expected user:password)", pair)
			continue
		}

		username := strings.TrimSpace(parts[0])
		password := strings.TrimSpace(parts[1])
		if username == "" || password == "" {
			log.Warnf("web: skipping empty username or password")
			continue
		}

		credentials[username] = password
	}

	return credentials
}

// basicAuthValidator returns a function that validates basic auth credentials
func basicAuthValidator(credentials map[string]string) func(string, string, echo.Context) (bool, error) {
	return func(username, password string, c echo.Context) (bool, error) {
		if expectedPassword, exists := credentials[username]; exists {
			return expectedPassword == password, nil
		}
		return false, nil
	}
}
