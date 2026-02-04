package utils

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Helper function to normalize phone numbers for WhatsApp
func NormalizePhoneNumber(phone string) string {
	// Remove all non-digit characters
	phone = strings.TrimSpace(phone)
	phone = regexp.MustCompile(`[^\d]`).ReplaceAllString(phone, "")

	// Handle Indonesian phone numbers
	if strings.HasPrefix(phone, "0") {
		phone = "62" + phone[1:] // Convert 08... to 628...
	} else if strings.HasPrefix(phone, "62") {
		// Already correct format
	} else if strings.HasPrefix(phone, "+62") {
		phone = phone[1:] // Remove +
	}

	return phone
}

func CalculateEndTime(startTime string, durationHours float64) string {
	const timeLayout = "15:04"
	t, err := time.Parse(timeLayout, startTime)
	if err != nil {
		fmt.Printf("⚠️ Invalid time format: %v\n", err)
		return startTime
	}

	duration := time.Duration(durationHours * float64(time.Hour))
	endTime := t.Add(duration)
	return endTime.Format(timeLayout)
}

// GetNextClassDate calculates the next occurrence of a specific day and time
func GetNextClassDate(dayOfWeek string, startTime time.Time) time.Time {
	dayMap := map[string]time.Weekday{
		"minggu": time.Sunday,
		"senin":  time.Monday,
		"selasa": time.Tuesday,
		"rabu":   time.Wednesday,
		"kamis":  time.Thursday,
		"jumat":  time.Friday,
		"sabtu":  time.Saturday,
	}

	targetDay, ok := dayMap[strings.ToLower(dayOfWeek)]
	if !ok {
		// Fallback to next week same day if invalid
		return time.Now().AddDate(0, 0, 7)
	}

	now := time.Now()
	currentDay := now.Weekday()

	// Calculate days until target
	daysUntil := int(targetDay - currentDay)
	if daysUntil < 0 {
		daysUntil += 7
	}

	// If it's today, check if the time has passed
	if daysUntil == 0 {
		todayClassTime := time.Date(
			now.Year(), now.Month(), now.Day(),
			startTime.Hour(), startTime.Minute(), 0, 0, time.Local,
		)
		// If class time has passed today, schedule for next week
		if now.After(todayClassTime) {
			daysUntil = 7
		}
	}

	nextDate := now.AddDate(0, 0, daysUntil)
	return time.Date(
		nextDate.Year(),
		nextDate.Month(),
		nextDate.Day(),
		startTime.Hour(),
		startTime.Minute(),
		0, 0, time.Local,
	)
}

// GetDayName returns Indonesian day name from time.Weekday
func GetDayName(weekday time.Weekday) string {
	dayNames := map[time.Weekday]string{
		time.Sunday:    "Minggu",
		time.Monday:    "Senin",
		time.Tuesday:   "Selasa",
		time.Wednesday: "Rabu",
		time.Thursday:  "Kamis",
		time.Friday:    "Jumat",
		time.Saturday:  "Sabtu",
	}
	return dayNames[weekday]
}
