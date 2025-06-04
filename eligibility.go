package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"go.elastic.co/apm"
)

type EligibilityRequest struct {
	Host     string
	Context  CDSContext
	Headers  map[string]string
	mu       sync.Mutex
	Data     *Data
	Maps     *Maps
	Criteria Criteria
}

type CDSContext struct {
	RequestContext context.Context
	Patient        Patient
	Encounter      map[string]string
	User           string
	Body           string
}

type Data struct {
	Appointments                 []*Appointment
	Encounters                   []*Encounter
	Medications                  map[string]*Medication
	MedicationRequests           []*MedicationRequest
	BiologicMedicationRequests   []*MedicationRequest
	ControllerMedicationRequests []*MedicationRequest
	ControllerCourses            [][]*MedicationRequest
	SteroidMedicationRequests    []*MedicationRequest
	SteroidCourses               [][]*MedicationRequest
	ProblemList                  []*Condition
	HospitalProblems             []*Condition
	EncDiagnosis                 []*Condition
	HospitalProblemList          []*List
	AsthmaActionPlan             AsthmaActionPlan
	AsthmaControlTool            AsthmaControlTool
}

type Maps struct {
	EncToMed       map[string]string
	MedicationType map[string]map[string]int
	CSNStatus      map[string]string
	EncDate        map[string]Date
}

type MedicationMap struct {
	Medication *Medication
	Category   []string
}

type AsthmaActionPlan struct {
	GreenZone  []*Observation
	YellowZone []*Observation
}

type AsthmaControlTool struct {
	Observations []*Observation
	Status       int64
	Date         time.Time
}

type Criteria struct {
	AsthmaRegistry *AsthmaRegistryCriteria
	SmartEligible  *SmartEligibleCriteria
	SmartInitiated *SmartInitiatedCriteria
}

func eligibility(c echo.Context) error {

	// Obtains raw http request
	r := c.Request()

	// Obtains http request context
	ctx := r.Context()

	hookRequest, err := parseCDSHooksRequest(r.Body)
	if err != nil {
		logger(ctx, err)
		return c.NoContent(http.StatusInternalServerError)
	}

	headers := map[string]string{
		"Authorization":   "Bearer " + hookRequest.FHIRAuthorization.AccessToken,
		"Accept":          "application/json",
		"Accept-Encoding": "gzip",
		"Content-Type":    "application/json",
	}

	// Remove access token from response to avoid storing this in the logs
	hookRequest.FHIRAuthorization.AccessToken = ""

	// Convert hook response back to string add as context for logs
	hookRequestBytes, err := json.Marshal(hookRequest)
	if err != nil {
		// Log an error if this fails, but continue to process request
		logger(ctx, fmt.Errorf("failed to marhsal hooks message: %v", err))
	}

	// Initialize eligibility request struct
	er := EligibilityRequest{
		Data: &Data{
			Medications: map[string]*Medication{},
		},
		Maps: &Maps{
			EncToMed:  map[string]string{},
			CSNStatus: map[string]string{},
			MedicationType: map[string]map[string]int{
				"antiasthmatic": {},
				"biologic":      {},
				"controller":    {},
				"steroid":       {},
			},
			EncDate: map[string]Date{},
		},
		Context: CDSContext{
			RequestContext: ctx,
			Patient: Patient{
				Id: hookRequest.Context.PatientId,
			},
			Encounter: map[string]string{
				"id":  hookRequest.Context.EncounterId,
				"csn": "",
			},
			User: hookRequest.Context.UserId,
			Body: string(hookRequestBytes),
		},
		Headers: headers,
		Host:    hookRequest.FHIRServer,
	}

	// Get patient data
	if err := er.getData(headers); err != nil {
		// Reporting of errors is handled in the individual functions so no further reporting done here.
		return c.NoContent(http.StatusInternalServerError)
	}

	// Evaluate asthma registry criteria
	er.Criteria.AsthmaRegistry = er.asthmaRegistry()

	// Convert struct to map to pass to generateCardDetail function
	detailMap := structToMap(*er.Criteria.AsthmaRegistry)

	// Build detail string
	detail, err := generateCardDetail(detailMap, "static/cardDetail.txt")
	if err != nil {
		logger(ctx, fmt.Errorf("%v (patient: %s)", err, er.Context.Patient.Id))
		return c.NoContent(http.StatusInternalServerError)
	}

	// Log evaluation results
	er.sendWebLog(detail)

	// Build basic Hook response
	hook := Hook{
		Cards:         []Card{},
		SystemActions: []SystemActions{},
	}

	// Patient has asthma. Evaluate SMART criteria
	if er.Criteria.AsthmaRegistry.Evaluation {

		er.Criteria.SmartEligible = er.smartEligible()
		er.Criteria.SmartInitiated = er.smartInitiated()

		// Patient meets criteria, build care to display to user
		if er.Criteria.SmartEligible.Evaluation && !er.Criteria.SmartInitiated.Evaluation {

			// Build RTF
			alertText := er.buildRTF()

			// Save RTF to EHR
			if err := er.saveState(config.AlertTextLocation, alertText, headers); err != nil {
				logger(ctx, fmt.Errorf("%v (patient: %s)", err, er.Context.Patient.Id))
				return c.NoContent(http.StatusInternalServerError)
			}

			// Add card
			hook.addCard(detail)

			// Add order set suggestion
			hook.addOrderSetSuggestion(0, er.Context.Patient.Id)
		}
	}

	// Return response
	return c.JSON(http.StatusOK, hook)
}

func (er *EligibilityRequest) getData(headers map[string]string) error {
	// Create elastic span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Combined")
	defer span.End()

	// Wait group for "top-level" requests
	var wg sync.WaitGroup
	wg.Add(6)

	// Create error channel one for each actual call, which includes
	//   MedicationRequest, Encounter, Appointment, Condition (Problems),
	//   List (Hospital Problem List), Patient, Condition (Encounter Diagnosis)
	//   QuestionnaireResponse (Asthma Control Tool), Observation (Asthma Action Plan)
	errCh := make(chan error, 9)

	// Get data. Encounter diagnosis requests are nested within the getEncounter function
	go er.getMedications(&wg, errCh, headers)
	go er.getPatient(&wg, errCh, headers)

	// Group problems together since both are needed to differentiate problem list from hospital problem list
	go er.getProblems(&wg, errCh, headers)

	// Group visits together since both are needed to identify visits of interest
	go er.getVisits(&wg, errCh, headers)

	// Get questionnaire repsonses for Asthma Control Tool
	go er.getAsthmaControlTool(&wg, errCh, headers)

	// Get asthma action plan
	go er.getAsthmaActionPlan(&wg, errCh, headers)

	// Wait for data before proceeding and close error channel
	go func() {
		wg.Wait()
		close(errCh)
	}()

	// Check for errors errors as they occur
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}

func (er *EligibilityRequest) getVisits(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Wait group for "top-level" requests
	var visitWg sync.WaitGroup
	visitWg.Add(2)

	// Create error channel
	visitErrCh := make(chan error, 2)

	// Send visit-related requests
	go er.getEncounters(&visitWg, visitErrCh, headers)
	go er.getAppointments(&visitWg, visitErrCh, headers)

	// Wait for data before proceeding and close error channel
	go func() {
		visitWg.Wait()
		close(visitErrCh)
	}()

	// Process for errors as they return
	for err := range visitErrCh {
		errCh <- err
	}

	// Process encounters
	er.processEncounters()

	// Get encounter Ids to retrieve encounter diagnosis
	encIdList := er.getEncounterDxList(-365)

	if len(encIdList) > 0 {
		if err := er.getEncounterDiagnoses(encIdList, headers); err != nil {
			errCh <- err
		}
	}
}

func (er *EligibilityRequest) getProblems(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Wait group for "top-level" requests
	var problemWg sync.WaitGroup
	problemWg.Add(2)

	// Create error channel
	problemErrCh := make(chan error, 2)

	// Send problem-related requests
	go er.getProblemList(&problemWg, problemErrCh, headers)
	go er.getHospitalProblemList(&problemWg, problemErrCh, headers)

	// Wait for data before proceeding and close error channel
	go func() {
		problemWg.Wait()
		close(problemErrCh)
	}()

	// Process for errors as they return
	for err := range problemErrCh {
		errCh <- err
	}

	// Process problems
	er.processProblems()
}
