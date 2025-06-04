package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"text/template"
	"time"
)

type Hook struct {
	Cards         []Card          `json:"cards,omitempty"`
	SystemActions []SystemActions `json:"systemActions"`
}

type Card struct {
	UUID              string            `json:"uuid"`
	Summary           string            `json:"summary"`
	Detail            string            `json:"detail"`
	Indicator         string            `json:"indicator"`
	Source            Source            `json:"source"`
	SelectionBehavior string            `json:"selectionBehavior"`
	Extension         *Extension        `json:"extension"`
	Links             []Link            `json:"links"`
	OverrideReasons   []OverrideReasons `json:"overrideReasons"`
	Suggestions       []Suggestion      `json:"suggestions"`
}

type Source struct {
	Label string  `json:"label"`
	URL   string  `json:"url"`
	Topic *Coding `json:"topic"`
}

type Extension struct {
	ContentType string `json:"com.epic.cdshooks.card.detail.content-type"`
}

type Suggestion struct {
	Label   string   `json:"label"`
	UUID    string   `json:"uuid"`
	Actions []Action `json:"actions"`
}

type Action struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Resource    interface{} `json:"resource"`
}

type MedicationRequestAction struct {
	ResourceType              string            `json:"resourceType"`
	Status                    string            `json:"status"`
	Intent                    string            `json:"intent"`
	Category                  []Category        `json:"category"`
	MedicationCodeableConcept Category          `json:"medicationCodeableConcept"`
	Subject                   ResourceReference `json:"subject"`
}

type ServiceRequestAction struct {
	ResourceType string            `json:"resourceType"`
	Status       string            `json:"status"`
	Intent       string            `json:"intent"`
	Category     []Category        `json:"category"`
	Code         Category          `json:"code"`
	Subject      ResourceReference `json:"subject"`
}

type ConditionAction struct {
	ResourceType string            `json:"resourceType"`
	Category     []Category        `json:"category"`
	Code         Category          `json:"code"`
	Subject      ResourceReference `json:"subject"`
}

type ServiceRequest struct {
	ResourceType string            `json:"resourceType"`
	Status       string            `json:"status"`
	Intent       string            `json:"intent"`
	Category     []Category        `json:"category"`
	Code         Category          `json:"code"`
	Subject      ResourceReference `json:"subject"`
}

type OverrideReasons struct {
	Coding []Coding `json:"coding"`
}

type SystemActions struct {
	// Define fields if needed
}

func parseCDSHooksRequest(body io.Reader) (HookRequest, error) {

	reqBytes, err := io.ReadAll(body)
	if err != nil {
		return HookRequest{}, err
	}

	// Unmarshal response into struct
	var hookRequest HookRequest
	if err := json.Unmarshal(reqBytes, &hookRequest); err != nil {
		return HookRequest{}, fmt.Errorf("unable to unmarhsal hooks message: %v", err)
	}

	return hookRequest, nil
}

func generateCardDetail(m map[string]string, fileName string) (string, error) {
	tmpl := template.Must(template.ParseFiles(fileName))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, m); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func structToMap(s any) map[string]string {
	// Initialize map
	result := make(map[string]string)

	// Create value and type fields
	val := reflect.ValueOf(s)
	typ := reflect.TypeOf(s)

	// Iterate over struct fields
	for i := range val.NumField() {
		field := typ.Field(i)
		value := val.Field(i)

		// Convert valus to string
		var strValue string
		switch value.Kind() {
		case reflect.Bool:
			strValue = strconv.FormatBool(value.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(value.Int(), 10)
		case reflect.String:
			strValue = value.String()
		default:
			strValue = fmt.Sprintf("%v", value.Interface()) // Fallback for other types
		}

		// Append result to map
		result[field.Name] = strValue
	}
	return result
}

func (h *Hook) addCard(detail string) {
	// Get string formated time
	formattedTime := time.Now().Format("20060102150405")

	// Build card
	h.Cards = append(h.Cards, Card{
		Summary:   "Patient Eligible for SMART Asthma Therapy",
		Indicator: "info",
		Extension: &Extension{
			ContentType: "text/html",
		},
		Detail: "<p hidden>" + detail + "</p>",
		Source: Source{
			Topic: &Coding{
				Code: fmt.Sprintf("SMARTAsthma%s", formattedTime),
			},
		},
	})
}

func (h *Hook) addSuggestion(card int) {
	// Check if suggestions list exists, if not, build it
	if h.Cards[card].Suggestions == nil {
		h.Cards[card].Suggestions = []Suggestion{}
	}
}

func (h *Hook) addOrderSetSuggestion(card int, patId string) {
	// Check if suggestions list exists, if not, build it
	if h.Cards[card].Suggestions == nil {
		h.addSuggestion(card)
	}

	// Build MedicationRequest suggestion
	suggestion := Suggestion{
		Label: "SMART Asthma SmartSet",
		UUID:  "order-set-request-test",
		Actions: []Action{
			{
				Type:        "create",
				Description: "SMART Asthma Therapy",
				Resource: ServiceRequest{
					ResourceType: "ServiceRequest",
					Status:       "draft",
					Intent:       "proposal",
					Category: []Category{
						{
							Coding: []Coding{
								{
									System:  "http://terminology.hl7.org/CodeSystem/medicationrequest-category",
									Code:    "outpatient",
									Display: "Outpatient",
								},
							},
						},
					},
					Code: Category{
						Coding: []Coding{
							{
								System: "urn:com.epic.cdshooks.action.code.system.orderset-item",
								Code:   config.OrderSetKey,
							},
						},
					},
					Subject: ResourceReference{
						Reference: fmt.Sprintf("Patient/%s", patId),
					},
				},
			},
		},
	}

	// Append suggestion to current suggestion list
	h.Cards[card].Suggestions = append(h.Cards[card].Suggestions, suggestion)
}
