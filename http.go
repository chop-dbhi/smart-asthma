package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	globalTimeout int
)

type Request struct {
	Method      string
	URL         string
	QueryParams url.Values
	Body        io.Reader
	Headers     map[string]string
}

type ResponseResult struct {
	Response *http.Response
	Error    error
	Body     []byte
}

func sendRequest(method, url string, queryParams url.Values, headers map[string]string, body io.Reader, timeout ...int) (*http.Response, error) {
	// Get timeout value, if passed, or use environment variable
	t := globalTimeout
	if len(timeout) > 0 {
		t = timeout[0]
	}

	// Create new HTTP client with timeout
	client := http.Client{
		Timeout: time.Duration(time.Duration(t) * time.Second),
	}

	// Create a new request
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// Set query parameters if provided
	if queryParams != nil {
		req.URL.RawQuery = queryParams.Encode()
	}

	// Set headers if provided
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Initiate request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	// Initialize re-used variables
	var respBody []byte
	var err error

	// Read the body and set up a defer to close the body to avoid
	// leaking resources.
	defer resp.Body.Close()

	// Check for gzipped "Content-Encoding" header
	if resp.Header.Get("Content-Encoding") == "gzip" {
		// Decompress response body
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error creating gzip reader: %s", err)
		}
		defer gzipReader.Close()

		// Read decompressed content
		respBody, err = io.ReadAll(gzipReader)
		if err != nil {
			return nil, fmt.Errorf("error reading decompressed data: %s", err)
		}
	} else {
		// Assume decompressed data
		respBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %s", err)
		}
	}
	return respBody, nil
}

func splitRequest(api string, split, lookback int, queryParams url.Values, headers map[string]string) []Request {

	// Init request list
	var requestList = []Request{}

	// Create time windows
	windows := createTimeWindows(split, lookback)

	// Iterate over windows
	for _, window := range windows {
		// Create a local copy of query parameters
		queryParamsLocal := url.Values{}
		for key, values := range queryParams {
			queryParamsLocal[key] = append([]string{}, values...)
		}
		addDateParam(window, &queryParamsLocal)
		request := Request{
			Method:      "GET",
			URL:         api,
			QueryParams: queryParamsLocal,
			Body:        nil,
			Headers:     headers,
		}
		requestList = append(requestList, request)
	}

	return requestList
}

func sendAll(requestList []Request, headers map[string]string, responseResults chan<- ResponseResult, wg *sync.WaitGroup) {

	// Iterate over requests and send in parallel
	for _, request := range requestList {
		// Increment the wait group with the new request
		wg.Add(1)

		// Send request asynchronously using a goroutine
		go func(request Request, headers map[string]string) {
			defer wg.Done()

			// Send request
			resp, err := sendRequest(request.Method, request.URL, request.QueryParams, headers, request.Body)

			// Send response or error back to channel
			responseResults <- ResponseResult{Response: resp, Error: err}
		}(request, headers)
	}
}

func (er *EligibilityRequest) processResults(responseResults chan ResponseResult) ([]ResponseResult, error) {
	// Boolean to store if an error occurred during any transaction
	var isError bool
	var responses []ResponseResult

	// Process results as they arrive
	for result := range responseResults {
		if result.Error != nil {
			isError = true
			logger(er.Context.RequestContext, fmt.Errorf("%v (patient: %s)", result.Error, er.Context.Patient.Id))
			continue
		}
		// Shortcut reference to http response
		response := result.Response

		// Extract response
		var err error
		result.Body, err = readBody(response)
		if err != nil {
			isError = true
			logger(er.Context.RequestContext, fmt.Errorf("%v (patient: %s)", err, er.Context.Patient.Id))
		}

		// Verify status code
		if response.StatusCode >= 400 {
			isError = true
			logger(er.Context.RequestContext, fmt.Errorf("request %s failed (%d): %s (context: %s)", response.Request.URL, response.StatusCode, string(result.Body), string(er.Context.Body)))
		} else {
			responses = append(responses, result)
		}
	}

	if isError {
		return nil, fmt.Errorf("error retrieving patient data")
	}
	return responses, nil
}

func (er *EligibilityRequest) sendAndProcess(requestList []Request, headers map[string]string) error {
	// Create sub-wait group
	var subWg sync.WaitGroup

	// Establish response channel
	responseCh := make(chan ResponseResult, len(requestList))

	// Send all requests
	sendAll(requestList, headers, responseCh, &subWg)

	// Close channel once all goroutines are finished
	go func() {
		subWg.Wait()
		close(responseCh)
	}()

	// Process results, checking for errors
	responses, err := er.processResults(responseCh)
	if err != nil {
		return err
	}

	// Parse response into FHIR structs
	for _, result := range responses {
		if err := er.processFHIRResponse(result.Body); err != nil {
			logger(er.Context.RequestContext, fmt.Errorf("%v (patient: %s)", err, er.Context.Patient.Id))
			return err
		}
	}

	return nil
}

func createChunks(values []string, chunkSize int) [][]string {
	var chunks [][]string
	for chunkSize < len(values) {
		values, chunks = values[chunkSize:], append(chunks, values[0:chunkSize:chunkSize])
	}
	return append(chunks, values)
}

func stripFHIRSuffix(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	// Normalize and check suffix
	suffix := "/FHIR/R4"

	// Check for trailing slashes
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, suffix)

	return u.String(), nil
}
