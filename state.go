package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.elastic.co/apm"
)

// Request body to store data to the EHR
type SaveRequestBody struct {
	ContextName     string
	EntityID        string
	EntityIDType    string
	ContactID       string
	ContactIDType   string
	UserID          string
	UserIDType      string
	Source          string
	SmartDataValues []SmartDataValues
}

type SmartDataValues struct {
	SmartDataID     string
	SmartDataIDType string
	Comments        []string
	Values          []string
}

func (er *EligibilityRequest) saveState(location, value string, headers map[string]string) error {
	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Save Data")
	defer span.End()

	// Remove FHIR path from host
	url, err := stripFHIRSuffix(er.Host)
	if err != nil {
		return err
	}

	// Build request body
	saveRequest := SaveRequestBody{
		ContextName:   "Encounter",
		EntityID:      er.Context.Patient.Id,
		EntityIDType:  "FHIR",
		ContactID:     er.Context.Encounter["csn"],
		ContactIDType: "CSN",
		UserID:        config.SystemUser,
		UserIDType:    "FHIR",
		Source:        "SMART Asthma Service",
		SmartDataValues: []SmartDataValues{
			{
				SmartDataID:     location,
				SmartDataIDType: "SDI",
				Comments: []string{
					"Set by the SMART Asthma eligibility service",
				},
				Values: []string{
					string(value),
				},
			},
		},
	}

	// Create bodyReader
	bodyReader, err := json.Marshal(saveRequest)
	if err != nil {
		return err
	}

	// Set URL
	url += "/epic/2013/Clinical/Utility/SETSMARTDATAVALUES/SmartData/Values"

	// Get encounter location
	resp, err := sendRequest(http.MethodPut, url, nil, headers, bytes.NewReader(bodyReader))
	if err != nil {
		return err
	}

	respBody, err := readBody(resp)
	if err != nil {
		return err
	}

	// Verify status code
	// If this succeeds, the token is likely valid
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("EHR write failed (Status Code - %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (er *EligibilityRequest) buildRTF() string {
	// Set base RTF
	rtf := `{\rtf1\ansi{\colortbl;\red0\green128\blue0;\red255\green0\blue0;}{\fonttbl\f0\fArial;}\fs22`

	// Set encodings for checkboxes
	empty := `\u9744`
	filled := `\u9745`

	// Set default values
	icon := empty
	dateString := ""

	// Check for SCS criteria
	if er.Criteria.SmartEligible.SCS183 && er.Criteria.SmartEligible.SCSEpisode365 {
		icon = filled
		var dates []string
		for _, t := range er.Criteria.SmartEligible.SCSDates {
			dates = append(dates, t.Format("01/02/2006"))
		}
		dateString = " (" + strings.Join(dates, ", ") + ")"
	}
	rtf += fmt.Sprintf("{\\fs28  %s}{  >= 2 rx for systemic steroids in last 365 days%s}\\line", icon, dateString)

	// Reset default values
	icon = empty
	dateString = " (Not on file in last 6 months)"
	if !er.Data.AsthmaControlTool.Date.IsZero() {
		dateString = " (" + er.Data.AsthmaControlTool.Date.Format(("01/02/2006")) + ")"
	}
	if er.Criteria.SmartEligible.UncontrolledACT {
		icon = filled
	}
	rtf += fmt.Sprintf("{\\fs28  %s}{  Poorly/Uncontrolled Asthma from Asthma Control Tool%s}", icon, dateString)

	// Close RTF section
	rtf += "}"

	return rtf
}
