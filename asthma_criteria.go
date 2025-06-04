package main

import (
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	asthmaICDRegex           = regexp.MustCompile(os.Getenv("ASTHMA_ICD_REGEX"))
	encounterTypeSystemRegex = regexp.MustCompile(os.Getenv("ENC_TYPE_SYSTEM_REGEX"))
)

type AsthmaRegistryCriteria struct {
	Alive            bool
	Encounter        bool
	Asthma           bool
	PersistentAsthma bool
	AsthmaMed        bool
	AsthmaEncDx      bool
	Evaluation       bool
}

func (er *EligibilityRequest) asthmaRegistry() *AsthmaRegistryCriteria {
	/*
	 * Patient is alive
	 * AND Patient not a test patient
	 * AND Office visit, hospital encounter, or emergency encounter in past 730 days
	 * AND (
	 *   (
	 *     (
	 *       Asthma on problem list
	 *       OR Asthma in encounter diagnosis within past 365 days
	 *     )
	 *     AND Medication order from Antiasthmatic pharmaceutical class within past 365 days
	 *   )
	 *   OR Persistent asthma diagnosis in problem list
	 * )
	 */

	// Set date values
	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)

	// Evaluate asthma registry criteria
	arc := AsthmaRegistryCriteria{}

	// Check if patient is alive
	arc.Alive = (er.Context.Patient.DeceasedDateTime == "")

	// Filter out hospital problems older than one year ago
	filteredHospitalProblems := er.filterHospitalProblemsByTime(er.Data.HospitalProblems, -365)

	// Check encounters over past 730 days
EncounterLoop:
	for _, encounter := range er.Data.Encounters {
		if encounter.Status != "cancelled" && encounter.Status != "noshow" {
			for _, coding := range encounter.Type {
				for _, code := range coding.Coding {
					if encounterTypeSystemRegex.MatchString(code.System) {
						if code.Code == "3" || code.Code == "101" || code.Code == "153" {
							arc.Encounter = true
							break EncounterLoop
						}
					}
				}
			}
		}
	}

	if !arc.Encounter {
	AppointmentLoop:
		// Check appointments over today - all other "completed" visits should be captured in encounters payload
		for _, appointment := range er.Data.Appointments {
			if isAfterDay(appointment.Period.Start.Time, yesterday) {
				if appointment.Status != "cancelled" && appointment.Status != "noshow" {
					arc.Encounter = true
					break AppointmentLoop
				}
			}
		}
	}

	// Check for asthma generally and persistent asthma on problem list
	// Using "active" problems as the source of truth for the problem list
AsthmaLoop:
	for _, problem := range er.Data.ProblemList {
		for _, code := range problem.Code.Coding {
			if code.System == "http://hl7.org/fhir/sid/icd-10-cm" {
				if asthmaICDRegex.MatchString(code.Code) {
					arc.Asthma = true
					if strings.Contains(strings.ToLower(code.Display), "persistent") {
						arc.PersistentAsthma = true
						break AsthmaLoop
					}
				}
			}
		}
	}

	// If asthma is on problem list, check if any asthma meds were ordered in last year
	if len(er.Maps.MedicationType["antiasthmatic"]) > 0 {
		arc.AsthmaMed = true
	}

	// Check for asthma visit diagnosis
EncDxLoop:
	for _, dx := range append(er.Data.EncDiagnosis, filteredHospitalProblems...) {
		for _, code := range dx.Code.Coding {
			if asthmaICDRegex.MatchString(code.Code) {
				arc.AsthmaEncDx = true
				break EncDxLoop
			}
		}
	}

	// Return final evaluation
	arc.Evaluation = arc.Alive && arc.Encounter && (((arc.Asthma || arc.AsthmaEncDx) && arc.AsthmaMed) || arc.PersistentAsthma)

	return &arc
}
