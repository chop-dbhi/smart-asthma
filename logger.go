package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"go.elastic.co/apm"
	"go.elastic.co/apm/module/apmechov4"
	"go.elastic.co/apm/module/apmzap"
	"go.uber.org/zap"
)

var (
	zapLogger *zap.Logger
	appEnv    string = os.Getenv("APP_ENV")
	appName   string = os.Getenv("APP_NAME")
	apmActive string = os.Getenv("ELASTIC_APM_ACTIVE")
	elkUrl    string = os.Getenv("ELK_URL")
)

func init() {

	// Set logging configuration
	var err error
	zapLogger, err = zap.NewProduction(zap.WrapCore((&apmzap.Core{}).WrapCore))
	if err != nil {
		log.Fatalf("Can't initialize zap logger: %v", err)
	}

	// Flushes buffer if it exists
	defer zapLogger.Sync()
}

func initAPM(e *echo.Echo) {
	// Close default Elastic APM tracer
	zapLogger.Info("Disable default APM logger")
	apm.DefaultTracer.Close()

	// Conditionally enable APM logger based on "ELASTIC_APM_ACTIVE" environment variable.
	if apmActive == "true" {
		// Create new tracer with basic options
		// Use environment variables for the remaining options
		zapLogger.Info("Creating new APM tracer",
			zap.String("ServiceName", appName),
			zap.String("ServiceEnvironment", appEnv))
		tracer, err := apm.NewTracerOptions(apm.TracerOptions{
			ServiceName:        appName,
			ServiceEnvironment: appEnv,
		})
		if err != nil {
			zapLogger.Fatal(err.Error())
		}

		// Adds elastic APM middleware to web server to capture requests
		// and send them to elastic
		zapLogger.Info("Enabling APM logger")
		e.Use(apmechov4.Middleware(apmechov4.WithTracer(tracer)))
	}
}

func logger(c context.Context, err error) {
	zapLogger.Error(err.Error())
	if apmActive == "true" {
		apm.CaptureError(c, err).Send()
	}
}

func elkLogger(msg map[string]string, level string) error {
	// Set default level if none exists
	if level == "" {
		level = "debug"
	}

	// Sends logs to a test index, if not production
	index := appEnv
	if index != "prod" {
		index = "test"
	}

	// Timestamp in ISO format
	datetime := time.Now().Format(time.RFC3339)

	// Populate remaining message details
	msg["environment"] = index
	msg["level"] = level
	msg["date"] = datetime

	// Build request body
	bodyReader, err := readerFromMap(msg)
	if err != nil {
		return err
	}

	// Set headers for request
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	// Send log message
	resp, err := sendRequest("POST", elkUrl, nil, headers, bodyReader, 5)
	if err != nil {
		return err
	}

	// Read the body
	body, err := readBody(resp)
	if err != nil {
		return err
	}

	// Verify status code
	if resp.StatusCode >= 400 {
		return fmt.Errorf("log message failed (Patient - %s, Status Code - %d): %s", msg["mrn"], resp.StatusCode, string(body))
	}

	return nil
}

func (er *EligibilityRequest) sendWebLog(msg string) {
	// Create base log message
	message := er.getWebLogContext()

	// Add log message to map
	message["msg"] = msg

	// Send log message in a separate thread to avoid slowing down the response
	go func() {
		// Send log message with current evaluation
		if err := elkLogger(message, "info"); err != nil {
			logger(er.Context.RequestContext, fmt.Errorf("%v. Context: %s ", err, er.Context.Body))
		}
	}()
}

func (er *EligibilityRequest) getWebLogContext() map[string]string {

	// Check for current CSN. If not available, pass FHIR id for encounter
	encId := er.Context.Encounter["csn"]
	if encId == "" {
		encId = er.Context.Encounter["id"]
	}

	// Return map with contextual details about the request
	return map[string]string{
		"application": appName,
		"mrn":         er.Context.Patient.MRN,
		"patFHIRId":   er.Context.Patient.Id,
		"encId":       encId,
	}
}

// Creates a string reader from a map
func readerFromMap(m map[string]string) (*strings.Reader, error) {
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(jsonBytes)), nil
}
