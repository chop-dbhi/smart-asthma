package main

import (
	"sync"

	"go.elastic.co/apm"
)

type Patient struct {
	ResourceType     string       `json:"resourcetype"`
	Id               string       `json:"id"`
	Identifier       []Identifier `json:"identifier"`
	Deceased         bool         `json:"deceasedBoolean"`
	DeceasedDateTime string       `json:"deceasedDateTime"`
	BirthDate        Date         `json:"birthDate"`
	MRN              string
}

func (er *EligibilityRequest) getPatient(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Patient")
	defer span.End()

	// Construct request and add to list
	requestList := []Request{
		{
			Method:      "GET",
			URL:         er.Host + "/Patient/" + er.Context.Patient.Id,
			QueryParams: nil,
			Body:        nil,
		},
	}

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		errCh <- err
		return
	}

	// Get patient identifiers
	er.getPatientIdentifiers()

	errCh <- nil
}

func (er *EligibilityRequest) getPatientIdentifiers() {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Extract MRN
	for _, identifier := range er.Context.Patient.Identifier {
		if identifier.Type["text"] == "EPI" {
			er.Context.Patient.MRN = identifier.Value
		}
	}
}
