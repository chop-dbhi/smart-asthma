package main

import (
	"fmt"
	"net/url"
	"sort"
	"time"
)

func createTimeWindows(splits int, lookback int) []map[string]string {
	// Get the start date
	start := time.Now().AddDate(0, 0, -lookback)

	// Gets initial step size
	baseStep := lookback / splits

	// Captures days that need to be distributed for uneven splits
	extraDays := lookback % splits

	// Create the windows as a slice of TimeWindow structs
	windows := []map[string]string{}
	dateFormat := "2006-01-02"

	for i := 0; i < splits; i++ {
		// Calculate the step (add extra days to the first few windows)
		step := baseStep
		if i < extraDays {
			step++
		}

		// Define the end time for the window
		end := start.AddDate(0, 0, step)

		// Add to the window
		windows = append(windows, map[string]string{
			"start": start.Format(dateFormat),
			"end":   end.Format(dateFormat),
		})

		// Reset starint date
		start = end
	}

	return windows
}

func addDateParam(window map[string]string, queryParms *url.Values) {
	// Set default values
	paramName := "date"
	var comparator string

	// Iterate over time window
	for key, value := range window {
		switch key {
		case "start":
			comparator = "ge"
		case "end":
			comparator = "le"
		}
		queryParms.Add(paramName, comparator+value)
	}
}

func isAfterDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()

	// Use the local timezone from t1
	loc := t1.Location()

	// Compare only the year, month, and day in the local timezone
	return time.Date(y1, m1, d1, 0, 0, 0, 0, loc).After(
		time.Date(y2, m2, d2, 0, 0, 0, 0, loc),
	)
}

func toDate(t time.Time) time.Time {
	return t.Truncate(24 * time.Hour)
}

func yearsBetween(start, end time.Time) int {
	// Get difference in years
	years := end.Year() - start.Year()

	// Adjust if end month or day is before start month or day
	if end.Month() < start.Month() || (end.Month() == start.Month() && end.Day() < start.Day()) {
		years--
	}

	return years
}

func filterByTime[T any](list []T, lookback int, getTime func(T) time.Time) []T {

	// Get values from past 365 days
	today := time.Now()
	lookbackDate := today.AddDate(0, 0, lookback)

	// Initialize the filtered list
	var filtered []T

	// Itearate over values in the list, extracting the time value based on the "getter" function
	for _, item := range list {
		t := getTime(item)
		if isAfterDay(t, lookbackDate) {
			filtered = append(filtered, item)
		}
	}

	return filtered
}

func sortEvents[T any](events []T, getTime func(T) time.Time, asc bool) []T {
	sort.Slice(events, func(i, j int) bool {
		t1 := getTime(events[i])
		t2 := getTime(events[j])
		if asc {
			return t1.Before(t2)
		} else {
			return !t1.Before(t2)
		}
	})
	return events
}

func groupEvents[T any](events []T, window int, getTime func(T) time.Time, asc bool) [][]T {
	if len(events) == 0 {
		return [][]T{}
	}

	// Sort orders chronologically
	events = sortEvents(events, getTime, asc)

	// Create slices to store results
	var grouped [][]T
	currentGroup := []T{events[0]}

	for i := 1; i < len(events); i++ {
		t1 := getTime(events[i])
		t2 := getTime(currentGroup[len(currentGroup)-1])
		// Convert to simple date
		currentDate := toDate(t1)
		previousDate := toDate(t2)

		// Check if current event is within the provided window
		if int((currentDate.Sub(previousDate).Abs().Hours() / 24)) <= window {
			currentGroup = append(currentGroup, events[i])
		} else {
			grouped = append(grouped, currentGroup)
			currentGroup = []T{events[i]}
		}
	}

	// Add the last group
	grouped = append(grouped, currentGroup)

	return grouped
}

func parseDate(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date: %s", s)
}
