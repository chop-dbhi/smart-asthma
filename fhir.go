package main

import (
	"encoding/json"
	"fmt"
)

// Simple struct to identify resourceType
type Resource struct {
	ResourceType string `json:"resourceType"`
}

type Bundle struct {
	ResourceType string `json:"resourceType"`
	Total        int    `json:"total"`
	Entry        []struct {
		FullUrl  string          `json:"fullUrl"`
		Resource json.RawMessage `json:"resource"`
	} `json:"entry"`
}

func (er *EligibilityRequest) processFHIRResponse(data []byte) error {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Unmarshal data into struct
	var resource Resource
	if err := json.Unmarshal(data, &resource); err != nil {
		return fmt.Errorf("failed to decode resourceType: %w", err)
	}

	// Check if it's a Bundle or a single resource
	switch resource.ResourceType {
	case "Bundle":
		return er.parseBundle(data)

	default:
		// Assume a single resource
		return er.parseResource(data)
	}
}

func (er *EligibilityRequest) parseBundle(data []byte) error {

	// Unmarshal top-level response information
	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf("error unmarshalling bundle: %s", err)
	}

	// Send individual entries to parse individually
	for _, entry := range bundle.Entry {
		if err := er.parseResource(entry.Resource); err != nil {
			return err
		}
	}

	return nil
}

func (er *EligibilityRequest) parseResource(data []byte) error {

	// Unmarshal data into struct
	var resource Resource
	if err := json.Unmarshal(data, &resource); err != nil {
		return fmt.Errorf("failed to decode resourceType: %w", err)
	}

	// Unmarshal based on resource type
	switch resource.ResourceType {
	case "Appointment":
		var appointment Appointment
		if err := json.Unmarshal(data, &appointment); err != nil {
			return fmt.Errorf("error unmarshalling Appointment: %s:%s", err, string(data))
		}
		er.Data.Appointments = append(er.Data.Appointments, &appointment)

	case "Condition":
		var condition Condition
		if err := json.Unmarshal(data, &condition); err != nil {
			return fmt.Errorf("error unmarshalling Condition: %s:%s", err, string(data))
		}
		for _, category := range condition.Category {
			if category.Text == "Problem List Item" {
				er.Data.ProblemList = append(er.Data.ProblemList, &condition)
			} else if category.Text == "Encounter Diagnosis" {
				er.Data.EncDiagnosis = append(er.Data.EncDiagnosis, &condition)
			}
		}

	case "Encounter":
		var encounter Encounter
		if err := json.Unmarshal(data, &encounter); err != nil {
			return fmt.Errorf("error unmarshalling Encounter: %s:%s", err, string(data))
		}
		er.Data.Encounters = append(er.Data.Encounters, &encounter)

	case "List":
		var list List
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("error unmarshalling List: %s:%s", err, string(data))
		}
		if list.Title == "Hospital Problem List" {
			er.Data.HospitalProblemList = append(er.Data.HospitalProblemList, &list)
		}

	case "Medication":
		var medication Medication
		if err := json.Unmarshal(data, &medication); err != nil {
			return fmt.Errorf("error unmarshalling Medication: %s:%s", err, string(data))
		}
		er.Data.Medications[medication.Id] = &medication

	case "MedicationRequest":
		var medicationRequest MedicationRequest
		if err := json.Unmarshal(data, &medicationRequest); err != nil {
			return fmt.Errorf("error unmarshalling MedicationRequest: %s:%s", err, string(data))
		}
		er.Data.MedicationRequests = append(er.Data.MedicationRequests, &medicationRequest)

	case "Observation":
		var observation Observation
		if err := json.Unmarshal(data, &observation); err != nil {
			return fmt.Errorf("error unmarshalling Observation: %s:%s", err, string(data))
		}
		for _, code := range observation.Code.Coding {
			if code.Code == config.AsthmaActionPlan.GreenZone {
				er.Data.AsthmaActionPlan.GreenZone = append(er.Data.AsthmaActionPlan.GreenZone, &observation)
			} else if code.Code == config.AsthmaActionPlan.YellowZone {
				er.Data.AsthmaActionPlan.YellowZone = append(er.Data.AsthmaActionPlan.YellowZone, &observation)
			} else {
				_, ok := config.AsthmaControlTool[code.Code]
				if ok {
					er.Data.AsthmaControlTool.Observations = append(er.Data.AsthmaControlTool.Observations, &observation)
				}
			}
		}

	case "Patient":
		if err := json.Unmarshal(data, &er.Context.Patient); err != nil {
			return fmt.Errorf("error unmarshalling Patient: %s:%s", err, string(data))
		}
	}
	return nil
}

// Define a generic function to remove duplicates based on a field.
func removeDuplicates[T any](slice []T, keyFunc func(T) any) []T {
	seen := make(map[interface{}]bool)
	var result []T

	// Iterate through the slice and use the keyFunc to get the key for deduplication.
	for _, item := range slice {
		key := keyFunc(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}

	return result
}
