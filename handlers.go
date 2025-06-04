package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

var (
	appVersion string
)

func cdsServices(c echo.Context) error {
	// Build basic Hook response
	serviceResponse := ServiceResponse{
		Services: []Service{
			{
				Hook:        "patient-view",
				Title:       "Check SMART Asthma Eligibility",
				Description: "Checks if a patient is eligible for SMART asthma therapy",
				Id:          "eligibility",
				Prefetch: map[string]string{
					"encounter":   "Encounter?patient={{context.patientId}}",
					"medications": "MedicationRequest?patient={{context.patientId}}",
				},
			},
		},
	}

	// Return response
	return c.JSON(http.StatusOK, serviceResponse)
}

func heartbeat(c echo.Context) error {
	// Heartbeat function to assess service status. Immediately return 200
	return c.NoContent(http.StatusOK)
}
