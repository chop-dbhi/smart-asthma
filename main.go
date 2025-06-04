package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var (
	config *Config
)

func init() {
	var err error

	// Extract necessary environment variables
	timeoutEnv := os.Getenv("TIMEOUT")
	appVersion = os.Getenv("APP_VERSION")

	// Set default value if not set
	if timeoutEnv == "" {
		globalTimeout = 30
	} else {
		// Convert timeout to integer
		globalTimeout, err = strconv.Atoi(timeoutEnv)
		if err != nil {
			log.Fatalf("Failed to convert timeout environment variable to integer")
		}
	}

	// Read list of requests
	config, err = readConfig()
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	// Create new Echo object
	e := echo.New()

	// Add basic middleware to log all requests
	e.Use(middleware.Logger())

	// Configure elastic apm logging
	initAPM(e)

	// Sets CORS headers to allow all origins, but restrict HTTP method type
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
	}))

	// Middleware to provide more control over response status for APM transactions
	// This must go after the Elastic APM middleware
	e.Use(filterError)

	// Adds a heartbeat handler
	e.GET("/heartbeat", heartbeat)

	// Creats API group to simplify middleware declaration
	cdsGroup := e.Group("/cds-services")

	// Add a GET handler for presenting the CDS Hooks services available
	cdsGroup.GET("", cdsServices)

	// Add a POST handler for CDS Hooks service
	cdsGroup.POST("/eligibility", eligibility, openId)

	// Start server
	e.Logger.Fatal(e.Start(":8000"))
}
