package main

import (
	"net/url"
	"os"
	"regexp"
	"sort"
	"sync"

	"go.elastic.co/apm"
)

// Should move to central config to support other medication vocabularies
var (
	antiasthmaticsGPIRegex = regexp.MustCompile(os.Getenv("ANTI_ASTHMATIC_REGEX"))
	biologicGPIRegex       = regexp.MustCompile(os.Getenv("BIOLOGIC_REGEX"))
	controllerRegex        = regexp.MustCompile(os.Getenv("CONTROLLER_REGEX"))
	icsfRegex              = regexp.MustCompile(os.Getenv("ICSF_REGEX"))
	steroidRegex           = regexp.MustCompile(os.Getenv("STEROID_REGEX"))
)

type MedicationRequest struct {
	ResourceType        string              `json:"resourcetype"`
	Id                  string              `json:"id"`
	Status              string              `json:"status"`
	Intent              string              `json:"intent"`
	MedicationReference MedicationReference `json:"medicationReference"`
	EncounterReference  ResourceReference   `json:"encounter"`
	AuthoredOn          Date                `json:"authoredOn"`
	Requester           ResourceReference   `json:"requester"`
	Recorder            ResourceReference   `json:"recorder"`
	DosageInstruction   []DosageInstruction `json:"dosageInstruction"`
	Class               string
	SubClass            string
}

type MedicationReference struct {
	ResourceReference
	VocabularyCode string
}

type DosageInstruction struct {
	Timing struct {
		Repeat struct {
			BoundsPeriod Period `json:"boundsPeriod"`
		} `json:"repeat"`
	} `json:"timing"`
	AsNeeded bool `json:"asNeededBoolean"`
}

type Medication struct {
	ResourceType string `json:"resourcetype"`
	Id           string `json:"id"`
	GPI          string
	Code         struct {
		Coding []Coding `json:"coding"`
	} `json:"code"`
}

func (er *EligibilityRequest) getMedications(wg *sync.WaitGroup, errCh chan<- error, headers map[string]string) {
	defer wg.Done()

	// Create span
	span, _ := apm.StartSpan(er.Context.RequestContext, "Get and Parse Data", "Medications")
	defer span.End()

	// Initialize query parameters
	queryParams := url.Values{}
	queryParams.Add("patient", er.Context.Patient.Id)
	queryParams.Add("intent", "order")
	queryParams.Add("status", "active,completed,stopped")
	queryParams.Add("_include", "MedicationRequest:medicationReference")

	// Split MedicationRequest resource
	requestList := splitRequest(er.Host+"/MedicationRequest", 2, 365, queryParams, headers)

	// Send requests and process responses
	if err := er.sendAndProcess(requestList, headers); err != nil {
		errCh <- err
		return
	}

	er.processMedications()

	errCh <- nil
}

func (er *EligibilityRequest) processMedications() {
	// Perform lock to avoid race conditions on shared data struct
	// If performance becomes a major issue, can further nest the structs so each data type
	// is operating on it's own struct
	er.mu.Lock()
	defer er.mu.Unlock()

	// Create data shortcut
	data := er.Data

	// Deduplicate returned values. Need to do this to remove duplicated medications across requests.
	data.MedicationRequests = removeDuplicates(data.MedicationRequests, func(mr *MedicationRequest) any {
		return mr.Id
	})

	// Sort by author date in reverse chronological order and then order status
	sort.Slice(data.MedicationRequests, func(i, j int) bool {
		if data.MedicationRequests[i].AuthoredOn == data.MedicationRequests[j].AuthoredOn {
			return data.MedicationRequests[i].Status < data.MedicationRequests[j].Status
		}
		return data.MedicationRequests[i].AuthoredOn.Time.After(data.MedicationRequests[j].AuthoredOn.Time)
	})

	// Link MedicationRequests to Medications
	er.linkMedicationCode()

	// Classify Medicaitons
	er.classifyMedications()

	// Filter medications based on additional criteria (e.g. ordering location)
	data.MedicationRequests = filterMedicationRequests(data.MedicationRequests)
}

func (er *EligibilityRequest) linkMedicationCode() {
	for _, mr := range er.Data.MedicationRequests {
		// Check if key exists
		med, ok := er.Data.Medications[mr.MedicationReference.Reference]
		if !ok {
			continue
		}
		// Check if we've already evaluated this medication
		if med.GPI == "" {
			// Loop through codes and add those of interest
			for _, code := range med.Code.Coding {
				if code.System == "urn:oid:2.16.840.1.113883.6.68" {
					med.GPI = code.Code
				}
			}
		}
		// Set the code on the MedicationRequest resource
		mr.MedicationReference.VocabularyCode = med.GPI
	}
}

// Classifies medication according to characteristics of the medication
func (er *EligibilityRequest) classifyMedications() {
	for _, mr := range er.Data.MedicationRequests {
		code := mr.MedicationReference.VocabularyCode
		if antiasthmaticsGPIRegex.MatchString(code) {
			er.Maps.MedicationType["antiasthmatic"][mr.Id] = 1
		}
		if biologicGPIRegex.MatchString(code) {
			er.Maps.MedicationType["biologic"][mr.Id] = 1
			er.Data.BiologicMedicationRequests = append(er.Data.BiologicMedicationRequests, mr)
		}
		if controllerRegex.MatchString(code) {
			er.Maps.MedicationType["controller"][mr.Id] = 1
			er.Data.ControllerMedicationRequests = append(er.Data.ControllerMedicationRequests, mr)
		}
		if steroidRegex.MatchString(code) {
			er.Maps.MedicationType["steroid"][mr.Id] = 1
			er.Data.SteroidMedicationRequests = append(er.Data.SteroidMedicationRequests, mr)
		}
	}
}

func filterMedicationRequests(medicationRequests []*MedicationRequest) []*MedicationRequest {
	var result []*MedicationRequest
	for _, mr := range medicationRequests {
		if mr.EncounterReference.Display != "Anesthesia" {
			result = append(result, mr)
		}
	}
	return result
}
