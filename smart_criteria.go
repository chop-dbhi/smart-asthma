package main

import (
	"time"
)

type SmartEligibleCriteria struct {
	Age               bool
	Biologic365       bool
	CCC               bool
	Controller30Days  bool
	Controller365Days bool
	SCS183            bool
	SCSEpisode365     bool
	SCSDates          []time.Time
	UncontrolledACT   bool
	Evaluation        bool
}

type SmartInitiatedCriteria struct {
	AAP        bool
	Age        bool
	ComboSig   bool
	ICSF       bool
	Evaluation bool
}

func (er *EligibilityRequest) smartEligible() *SmartEligibleCriteria {
	/*
	 * Age 5-18 years
	 * AND Active at start of month in Primary Care Wellness Registry
	 * AND Active in Asthma Registry (defined based on asthma_criteria.go). Always true if this function is evaluated.
	 * AND NOT Biologic order in previous 365 days
	 * AND < 3 complex chronic conditions (Chen to provide simple approach to capture this)
	 * AND ICS/L order in previous 365 days
	 * AND (
	 *   (
	 *     2 SCS prescribing episodes (defined as orders >= 14 days apart) in previous 365 days
	 *     AND >= 1 SCS order in the last 183 days at least 30 days after most recent ICS/L
	 *   )
	 *	 OR Uncontrolled Asthma Control Tool response in previous 183 days at least 30 days after most recent ICS/L
	 * )
	 *
	 * Definitions:
	 *   - ICS/L  = Inhaled corticosteroids, Tier 1 Controller Medication
	 *   - SCS = Glucocorticosteroid
	 *   - Recent = Oldest of the most "recent group" based on the timeframe defined by the medication (e.g. SCS - 14 days, ICS/L - 30 days)
	 */

	// Set date values
	today := time.Now()
	oneMonthLookback := today.AddDate(0, 0, -30)
	sixMonthLookback := today.AddDate(0, 0, -183)

	// Evaluate smart eligible criteria
	sec := SmartEligibleCriteria{}

	// Evalute a patient's age
	ageInYears := yearsBetween(er.Context.Patient.BirthDate.Time, today)
	if ageInYears >= 5 && ageInYears <= 18 {
		sec.Age = true
	}

	// Check for at least two SCS courses
	er.Data.SteroidCourses = groupEvents(er.Data.SteroidMedicationRequests, 14, func(mr *MedicationRequest) time.Time {
		return mr.AuthoredOn.Time
	}, false)
SCSCourseLoop:
	for i := len(er.Data.SteroidCourses) - 1; i >= 0; i-- {
		scsCourse := er.Data.SteroidCourses[i]
		// Check for oder within past 6 months
		if !sec.SCS183 {
			if isAfterDay(scsCourse[0].AuthoredOn.Time, sixMonthLookback) {
				sec.SCS183 = true
			}
		}

		// Add order dates to list, which will be displayed in UI
		sec.SCSDates = append(sec.SCSDates, scsCourse[0].AuthoredOn.Time)
		if len(er.Data.SteroidCourses)-i >= 2 {
			sec.SCSEpisode365 = true
			break SCSCourseLoop
		}
	}

	if er.Data.AsthmaControlTool.Status >= 2 {
		sec.UncontrolledACT = true
	}

	// Check for biologic order within past 365 days
	if len(er.Data.BiologicMedicationRequests) > 0 {
		sec.Biologic365 = true
	}

	// Check for controller order within past 30 days
	// Group medication orders based on a given window
	er.Data.ControllerCourses = groupEvents(er.Data.ControllerMedicationRequests, 30, func(mr *MedicationRequest) time.Time {
		return mr.AuthoredOn.Time
	}, false)
	if len(er.Data.ControllerCourses) >= 1 {
		sec.Controller365Days = true
	}

ControllerOrderLoop:
	for _, group := range er.Data.ControllerCourses {
		if isAfterDay(group[0].AuthoredOn.Time, oneMonthLookback) {
			sec.Controller30Days = true
			break ControllerOrderLoop
		}
	}

	// Return final evaluation
	sec.Evaluation = sec.Age && !sec.Biologic365 && !sec.Controller30Days && sec.Controller365Days && !sec.CCC && ((sec.SCSEpisode365 && sec.SCS183) || sec.UncontrolledACT)

	return &sec
}

func (er *EligibilityRequest) smartInitiated() *SmartInitiatedCriteria {
	/*
	 * Age 5-18 years
	 * AND Active at start of month in Primary Care Wellness Registry (this is built into the EHR config based on encounter restrictions)
	 * AND Active in Asthma Registry (defined based on asthma_criteria.go). Always true if this function is evaluated.
	 * AND ICS-Formoterol is most recent controller
	 * AND (
	 *   (
	 *     Order for ICS-Formoterol
	 *     AND Use of combined dosage/sig in the above order (Scheduled + PRN prescription) - using asNeededBoolean to determine
	 *   )
	 *	 OR ICS-Formoterol in Asthma Action Plan green AND yellow zone (Need to identify what cateogry values are listed here)
	 * )
	 *
	 * Definitions:
	 *   - ICS-Formoterol = Budesonide-Formoterol Fumarate and Mometasone Furo-Formoterol Fum
	 *   - Recent         = Most recent order
	 */

	// Set date values
	today := time.Now()

	// Evaluate smart eligible criteria
	sic := SmartInitiatedCriteria{}

	// Evalute a patient's age
	ageInYears := yearsBetween(er.Context.Patient.BirthDate.Time, today)
	if ageInYears >= 5 && ageInYears <= 18 {
		sic.Age = true
	}

	// Check if ICS-Formoterol is the last controller ordered
	if len(er.Data.ControllerMedicationRequests) > 0 {
		// List has been sorted in reverse chronological order and then by order status
		mr := er.Data.ControllerMedicationRequests[0]

		// Evaluate medication
		if icsfRegex.MatchString(mr.MedicationReference.VocabularyCode) {
			// Medication is an ICS-Formoterol
			sic.ICSF = true

			// Check for combination dose/signature, based on a PRN and scheduled dosage instruction
			if len(mr.DosageInstruction) == 2 {
				if mr.DosageInstruction[0].AsNeeded != mr.DosageInstruction[1].AsNeeded {
					sic.ComboSig = true
				}
			}
		}
	}

AsthmaActionPlanLoop:
	// Check for SMART medications across green and yellow zone of asthma action plan using IDs provided
	// by the health care organization
	// NOTE: This does not verify whether medication in plan is the same as the latest order
	for _, gzo := range er.Data.AsthmaActionPlan.GreenZone {
		for _, gz_component := range gzo.Component {
			for _, gz_coding := range gz_component.ValueCodeableConcept.Coding {
				yz_codes, ok := config.AsthmaActionPlan.MedicationMap[gz_coding.Code]
				if ok {
					for _, yzo := range er.Data.AsthmaActionPlan.YellowZone {
						for _, yz_component := range yzo.Component {
							for _, yz_coding := range yz_component.ValueCodeableConcept.Coding {
								for _, yz_code := range yz_codes {
									if yz_coding.Code == yz_code {
										sic.AAP = true
										break AsthmaActionPlanLoop
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Return final evaluation
	sic.Evaluation = sic.Age && sic.ICSF && (sic.ComboSig || sic.AAP)

	return &sic
}
