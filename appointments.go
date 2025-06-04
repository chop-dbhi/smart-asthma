package main

import (
	"net/url"
	"os"
	"regexp"
	"sync"

	"go.elastic.co/apm"
)

var (
	csnSystemRegex = regexp.MustCompile(os.Getenv("CSN_SYSTEM_REGEX"))
)

type Appointment struct {
	ResourceType string       `json:"resourcetype"`
	Id           string       `json:"id"`
	Identifier   []Identifier `json:"identifier"`
	Status       string       `json:"status"`
	Period       Period       `json:"period"`
	Start        Date         `json:"start"`
	End          Date         `json:"end"`
}

func (er *EligibilityRequest) getAppointments(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Appointments")
	defer span.End()

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)

	// Split MedicationRequest resource
	requestList := splitRequest(er.Host+"/Appointment", 2, 730, queryParams, headers)

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		errCh <- err
		return
	}

	// Process appointments
	er.processAppointments()

	errCh <- nil
}

func (er *EligibilityRequest) processAppointments() {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Create data shortcut
	data := er.Data

	// Deduplicate returned values
	data.Appointments = removeDuplicates(data.Appointments, func(a *Appointment) any {
		return a.Id
	})

	// Loop through appointments to build a map and remove appointments older than yesterday
	for _, appointment := range data.Appointments {
		for _, identifier := range appointment.Identifier {
			if csnSystemRegex.MatchString(identifier.System) {
				er.Maps.CSNStatus[identifier.Value] = appointment.Status
			}
		}
	}
}
