package main

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"go.elastic.co/apm"
)

type Observation struct {
	ResourceType string      `json:"resourcetype"`
	Id           string      `json:"id"`
	Issued       Date        `json:"issued"`
	Component    []Component `json:"component"`
	Code         struct {
		Coding []Coding `json:"coding"`
	} `json:"code"`
	Focus []ResourceReference
}

type Component struct {
	Text                 string `json:"text"`
	ValueCodeableConcept struct {
		Coding []Coding `json:"coding"`
		Text   string   `json:"text"`
	} `json:"valueCodeableConcept"`
	ValueQuantity struct {
		Value int64 `json:"value"`
	} `json:"valueQuantity"`
}

func (er *EligibilityRequest) getAsthmaActionPlan(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Asthma Action Plan")
	defer span.End()

	// Set values to look for
	// TODO - Need to change to AAP values (patient based)
	values := []string{
		config.AsthmaActionPlan.GreenZone,
		config.AsthmaActionPlan.YellowZone,
	}

	// Pre-pend OID to each value
	modified := []string{}
	for _, id := range values {
		modified = append(modified, config.ObservationOID+"|"+id)
	}

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)
	queryParams.Add("category", "smartdata")
	queryParams.Add("code", strings.Join(modified, ","))

	// Construct request and add to list
	requestList := []Request{
		{
			Method:      "GET",
			URL:         er.Host + "/Observation",
			QueryParams: queryParams,
			Body:        nil,
		},
	}

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		errCh <- err
		return
	}

	errCh <- nil
}

func (er *EligibilityRequest) getAsthmaControlTool(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Asthma Control Tool")
	defer span.End()

	// Create lookback period
	sixMonthLookback := time.Now().AddDate(0, 0, -183)
	dateFormat := "2006-01-02"

	// Pre-pend OID to each value
	modified := []string{}
	for id, _ := range config.AsthmaControlTool {
		modified = append(modified, config.ObservationOID+"|"+id)
	}

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)
	queryParams.Add("category", "smartdata")
	queryParams.Add("code", strings.Join(modified, ","))
	queryParams.Add("issued", "ge"+sixMonthLookback.Format(dateFormat))

	// Construct request and add to list
	requestList := []Request{
		{
			Method:      "GET",
			URL:         er.Host + "/Observation",
			QueryParams: queryParams,
			Body:        nil,
		},
	}

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		errCh <- err
		return
	}

	// Process Asthma Control Tool responses
	er.evaluateACT()

	errCh <- nil
}

func (er *EligibilityRequest) evaluateACT() {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Sort responses in reverse chronological order
	sort.Slice(er.Data.AsthmaControlTool.Observations, func(i, j int) bool {
		return er.Data.AsthmaControlTool.Observations[i].Issued.Time.After(er.Data.AsthmaControlTool.Observations[j].Issued.Time)
	})

	// Initialize encounter Id to match values to
	var encounterId string

	// Iterate over questionnaire responses to filter out those that aren't relevant
	for _, o := range er.Data.AsthmaControlTool.Observations {
		// Set encounterId, if not set
		if encounterId == "" {
			encounterId = o.Focus[0].Reference
		}

		// Store the time for when the ACT used for analysis was completed
		er.Data.AsthmaControlTool.Date = o.Issued.Time

		// Check if the focus is for the same encounter. Only pull the latest encounter
		// as determined by the latest encounter where a value was changed.
		if o.Focus[0].Reference == encounterId {
			for _, component := range o.Component {
				if component.ValueQuantity.Value > er.Data.AsthmaControlTool.Status {
					er.Data.AsthmaControlTool.Status = component.ValueQuantity.Value
				}
			}
		}
	}
}
