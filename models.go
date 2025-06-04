package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

/**************************
 ****** CDS Services ******
 **************************/
type ServiceResponse struct {
	Services []Service `json:"services"`
}

type Service struct {
	Hook              string            `json:"hook"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	Id                string            `json:"id"`
	Prefetch          map[string]string `json:"prefetch"`
	UsageRequirements string            `json:"usageRequirements"`
}

/**************************
 ****** Hook Message ******
 **************************/
type HookRequest struct {
	Hook              string `json:"hook"`
	HookInstance      string `json:"hookInstance"`
	FHIRServer        string `json:"fhirServer"`
	FHIRAuthorization struct {
		AccessToken string `json:"access_token"`
	} `json:"fhirAuthorization"`
	Context struct {
		PatientId   string `json:"patientId"`
		EncounterId string `json:"encounterId"`
		UserId      string `json:"userId"`
	}
}

/****************************************
 ****** Hook Response - Foundation ******
 ****************************************/

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Type  string `json:"type"`
}

type Coding struct {
	System  string `json:"system"`
	Code    string `json:"code"`
	Display string `json:"display"`
}

type Category struct {
	Coding []Coding `json:"coding"`
	Text   string   `json:"text"`
}

type ResourceReference struct {
	ResourceType string
	Reference    string `json:"reference"`
	Type         string `json:"type"`
	Display      string `json:"display"`
}

/*********************************
 ****** FHIR Nested Structs ******
 *********************************/

type Period struct {
	Start Date `json:"start"`
	End   Date `json:"end"`
}

// Create custom date type
type Date struct {
	time.Time
}

type Identifier struct {
	System string            `json:"system"`
	Value  string            `json:"value"`
	Use    string            `json:"use"`
	Type   map[string]string `json:"type"`
}

/********************************
 ********** App Config **********
 ********************************/

type Config struct {
	AlertTextLocation string                 `json:"alertTextLocation"`
	AsthmaActionPlan  AsthmaActionPlanConfig `json:"asthmaActionPlan"`
	ObservationOID    string                 `json:"observationOID"`
	AsthmaControlTool map[string]bool        `json:"asthmaControlTool"`
	OrderSetKey       string                 `json:"orderSetKey"`
	SystemUser        string                 `json:"systemUser"`
}

type AsthmaActionPlanConfig struct {
	GreenZone     string              `json:"greenZone"`
	YellowZone    string              `json:"yellowZone"`
	MedicationMap map[string][]string `json:"medicationMap"`
}

/*******************************
 ***** Unmarshal Functions *****
 *******************************/

// Custom UnmarshalJSON for Date type
func (r *ResourceReference) UnmarshalJSON(data []byte) error {

	// Create a temporary struct to hold raw data
	var temp struct {
		Reference string `json:"reference"`
		Type      string `json:"type"`
		Display   string `json:"display"`
	}

	// Unmarshal into the temporary struct
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Parse resource and ID from "ResourceType/ID"
	if parts := strings.SplitN(temp.Reference, "/", 2); len(parts) == 2 {
		r.ResourceType = parts[0]
		r.Reference = parts[1]
	}

	// Build other fields
	r.Type = temp.Type
	r.Display = temp.Display

	return nil
}

// Custom UnmarshalJSON for Date type
func (d *Date) UnmarshalJSON(data []byte) error {

	// Remove quotes around the date string
	dateStr := string(data)
	dateStr = dateStr[1 : len(dateStr)-1]

	// Parse string
	parsedTime, err := parseDate(dateStr)
	if err != nil {
		return fmt.Errorf("error parsing date: %v", err)
	}

	// Set parsed time to Date struct
	d.Time = parsedTime
	return nil
}
