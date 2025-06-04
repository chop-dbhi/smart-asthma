package main

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"go.elastic.co/apm"
)

type Condition struct {
	ResourceType   string `json:"resourcetype"`
	Id             string `json:"id"`
	ClinicalStatus struct {
		Coding []Coding `json:"coding"`
		Text   string   `json:"text"`
	} `json:"clinicalStatus"`
	Category []struct {
		Text string `json:"text"`
	} `json:"category"`
	Code struct {
		Coding []Coding `json:"coding"`
	} `json:"code"`
	EncounterReference ResourceReference `json:"encounter"`
}

type List struct {
	ResourceType       string            `json:"resourcetype"`
	Id                 string            `json:"id"`
	Title              string            `json:"title"`
	EncounterReference ResourceReference `json:"encounter"`
	Entry              []struct {
		Item ResourceReference `json:"item"`
	} `json:"entry"`
}

func (er *EligibilityRequest) getProblemList(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Problems")
	defer span.End()

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)
	queryParams.Add("category", "problem-list-item")

	// Construct request and add to list
	requestList := []Request{
		{
			Method:      "GET",
			URL:         er.Host + "/Condition",
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

func (er *EligibilityRequest) getHospitalProblemList(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Hospital Problems")
	defer span.End()

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)
	queryParams.Add("code", "hospital-problems")

	// Construct request and add to list
	requestList := []Request{
		{
			Method:      "GET",
			URL:         er.Host + "/List",
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

func (er *EligibilityRequest) getEncounterDiagnoses(encounterList []string, headers map[string]string) error {

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "EncounterDiagnosis")
	defer span.End()

	// Initialize a request list
	requestList := []Request{}

	// Send requests for each chunk
	for _, chunk := range createChunks(encounterList, 30) {

		// Initialize query parameters
		queryParams := url.Values{}
		queryParams.Add("patient", er.Context.Patient.Id)
		queryParams.Add("category", "encounter-diagnosis")
		queryParams.Add("encounter", strings.Join(chunk, ","))

		// Construct request and add to list
		request := Request{
			Method:      "GET",
			URL:         er.Host + "/Condition",
			QueryParams: queryParams,
			Body:        nil,
		}
		requestList = append(requestList, request)
	}

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		return err
	}

	return nil
}

func (er *EligibilityRequest) processProblems() {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Create data shortcut
	data := er.Data

	// Create a filtered slice based on the existing problem slice, re-using memory
	filtered := data.ProblemList[:0]

	// Iterate over the patient's problem list
	for _, condition := range data.ProblemList {
		// Keep the condition in the "ProblemList" struct, if active
		if condition.ClinicalStatus.Text == "Active" {
			filtered = append(filtered, condition)
		}
		// Check if the problem was incldued as a hospital problem, if so, add to hospital problems list
		// Required since the hospital problem list resource only includes references to conditions, not
		// the actual conditions themselves.
		for _, hospitalProblem := range data.HospitalProblemList {
			for _, entry := range hospitalProblem.Entry {
				if entry.Item.Reference == condition.Id {
					data.HospitalProblems = append(data.HospitalProblems, condition)
				}
			}
		}
	}

	// Replace problem list with filtered list (e.g. active problems)
	data.ProblemList = filtered
}

func (er *EligibilityRequest) filterHospitalProblemsByTime(list []*Condition, lookback int) []*Condition {

	// Initialize date values
	today := time.Now()
	lookbackDate := today.AddDate(0, 0, lookback)

	// Create a filtered slice based on the existing problem slice, re-using memory
	var filtered []*Condition

	// Iterate over encounters
	for _, problem := range list {

		// Check if csn exists in the encounter date map
		value, ok := er.Maps.EncDate[problem.EncounterReference.Reference]
		if ok {
			if isAfterDay(value.Time, lookbackDate) {
				filtered = append(filtered, problem)
			}
		}
	}

	// Replace hospital problem list with filtered problems
	return filtered
}
