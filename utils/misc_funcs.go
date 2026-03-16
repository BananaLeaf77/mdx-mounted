package utils

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

func TranslateDayOfWeek(dayOfWeek string) string {
	dayOfWeek = strings.ToLower(dayOfWeek)
	switch dayOfWeek {
	case "monday":
		return "Senin"
	case "tuesday":
		return "Selasa"
	case "wednesday":
		return "Rabu"
	case "thursday":
		return "Kamis"
	case "friday":
		return "Jumat"
	case "saturday":
		return "Sabtu"
	case "sunday":
		return "Minggu"
	default:
		return dayOfWeek
	}
}

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

func GetNextClassDate(dayOfWeek string, startTime time.Time) time.Time {
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		loc = time.Local
	}
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
		return time.Now().In(loc).AddDate(0, 0, 7)
	}
	now := time.Now().In(loc)
	currentDay := now.Weekday()

	daysUntil := int(targetDay - currentDay)
	if daysUntil < 0 {
		daysUntil += 7
	}

	nextDate := now.AddDate(0, 0, daysUntil)
	targetTime := time.Date(
		nextDate.Year(),
		nextDate.Month(),
		nextDate.Day(),
		startTime.Hour(),
		startTime.Minute(),
		0, 0, loc,
	)

	// H-6 rule: only skip to next week if the class is TODAY and starts within 6 hours
	// (or has already passed). A future day is always valid.
	if daysUntil == 0 && targetTime.Sub(now) < 6*time.Hour {
		targetTime = targetTime.AddDate(0, 0, 7)
	}

	return targetTime
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
