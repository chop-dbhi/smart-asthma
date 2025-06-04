package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"go.elastic.co/apm"
)

var (
	authHost string = os.Getenv("AUTH_HOST")
)

const (
	// Utilizes a non-standard nginx code
	statusClosedConnection int = 499
)

func filterError(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		resp := c.Response()
		// Process the request
		err := next(c)
		// The below is executed after the request and subsequent middleware
		if err != nil {
			// Check for a broken pipe, modify response status, and create an error
			if errors.Is(err, syscall.EPIPE) {
				logger(c.Request().Context(), err)
				resp.Status = statusClosedConnection
				return nil
			}
		}
		return err
	}
}

func openId(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Obtains raw http request
		r := c.Request()

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			logger(r.Context(), errors.New("authorization header not found"))
			return c.NoContent(http.StatusUnauthorized)
		}

		err := sendAuth("openid", authHeader, r)
		if err != nil {
			logger(r.Context(), err)
			return c.NoContent(http.StatusUnauthorized)
		}

		// Convert auth header to token and store on request object
		token, err := parseToken(authHeader)
		if err != nil {
			logger(r.Context(), err)
			return c.NoContent(http.StatusUnauthorized)
		}

		// Set token on context struct
		c.Set("user", token)

		// Otherwise return
		return next(c)
	}
}

func sendAuth(api string, authHeader string, r *http.Request) error {
	// Create span
	span, _ := apm.StartSpan(r.Context(), "Authorize Request", "OpenId")
	defer span.End()

	// Send http request to auth service
	// If it fails, fail the request
	// Create new HTTP client
	client := http.Client{
		Timeout: time.Duration(5 * time.Second),
	}

	// Create new request
	req, err := http.NewRequest("POST", authHost+api, nil)
	if err != nil {
		return err
	}

	// Add headers
	req.Header.Set("Authorization", authHeader)

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %v", err)
	}

	// Verify status code
	// If this succeeds, the token is likely valid
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Otherwise return
	return nil
}
