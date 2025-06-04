package main

import (
	"net/url"
	"sort"
	"sync"
	"time"

	"go.elastic.co/apm"
)

type Encounter struct {
	ResourceType string       `json:"resourcetype"`
	Id           string       `json:"id"`
	Status       string       `json:"status"`
	Identifier   []Identifier `json:"identifier"`
	Type         []struct {
		Coding []Coding `json:"coding"`
	} `json:"type"`
	Period           Period `json:"period"`
	AsthmaMedication bool
	AsthmaDiagnosis  bool
}

func (er *EligibilityRequest) getEncounters(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Encounters")
	defer span.End()

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)

	// Split MedicationRequest resource
	requestList := splitRequest(er.Host+"/Encounter", 2, 730, queryParams, headers)

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		errCh <- err
		return
	}

	errCh <- nil
}

func (er *EligibilityRequest) processEncounters() {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Deduplicate returned values
	er.Data.Encounters = removeDuplicates(er.Data.Encounters, func(e *Encounter) any {
		return e.Id
	})

	// Sort by author date in reverse chronological order and then order status
	sort.Slice(er.Data.Encounters, func(i, j int) bool {
		return er.Data.Encounters[i].Period.Start.Time.After(er.Data.Encounters[j].Period.Start.Time)
	})

	// Iterate over encounters
	for i, encounter := range er.Data.Encounters {
		// Add encounter to date map
		er.Maps.EncDate[encounter.Id] = encounter.Period.Start

		// Loop to extract CSN, if it exists
		var csn string
		for _, identifier := range encounter.Identifier {
			if csnSystemRegex.MatchString(identifier.System) {
				csn = identifier.Value
				if encounter.Id == er.Context.Encounter["id"] {
					er.Context.Encounter["csn"] = csn
				}
			}
		}
		// Check if csn exists in the appointment map
		value, ok := er.Maps.CSNStatus[csn]
		if ok {
			er.Data.Encounters[i].Status = value
		}
	}
}

func (er *EligibilityRequest) getEncounterDxList(lookback int) []string {
	// Initialize encounter dx Id list
	var encIdList []string

	// Initialize date values
	today := time.Now()
	oneYearAgo := today.AddDate(0, 0, lookback)

	// Iterate over encounters
	for _, encounter := range er.Data.Encounters {
		if isAfterDay(encounter.Period.Start.Time, oneYearAgo) {
			encIdList = append(encIdList, encounter.Id)
		}
	}

	return encIdList
}
