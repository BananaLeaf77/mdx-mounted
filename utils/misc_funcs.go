package utils

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nyaruka/phonenumbers"
)

// formatJID converts a WhatsApp JID like "6289529539993:49@s.whatsapp.net"
// into a readable Indonesian phone number like "+62 89529539993".
func FormatJID(jid string) string {
	jid = strings.TrimSpace(jid)

	// strip device suffix after "@" -> "6289529539993:49"
	if i := strings.Index(jid, "@"); i != -1 {
		jid = jid[:i]
	}

	// strip device id after ":" -> "6289529539993"
	if i := strings.Index(jid, ":"); i != -1 {
		jid = jid[:i]
	}

	if jid == "" {
		return ""
	}

	// insert a space after the country code
	if strings.HasPrefix(jid, "62") {
		return "+62 " + jid[2:]
	}

	return "+" + jid
}

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

func NormalizePhoneNumberIntl(raw string, countryCode string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("nomor telepon kosong")
	}
	if countryCode == "" {
		countryCode = "ID" // fallback for legacy data without a country set
	}

	num, err := phonenumbers.Parse(raw, countryCode)
	if err != nil {
		return "", fmt.Errorf("gagal mem-parsing nomor telepon: %w", err)
	}

	if !phonenumbers.IsValidNumber(num) {
		return "", fmt.Errorf("nomor telepon tidak valid untuk region %s", countryCode)
	}

	e164 := phonenumbers.Format(num, phonenumbers.E164) // "+818012345678"
	return strings.TrimPrefix(e164, "+"), nil
}

// NormalizePhoneNumber is kept for backward compatibility with existing
// call sites that don't yet pass a country code — assumes Indonesian
// numbers, matching the old hardcoded behavior. Prefer
// NormalizePhoneNumberIntl for anything touching a student/teacher record
// that has a CountryCode field available.
func NormalizePhoneNumber(raw string) string {
	normalized, err := NormalizePhoneNumberIntl(raw, "ID")
	if err != nil {
		return ""
	}
	return normalized
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

func CheckCancelAbleFromNow(classDate time.Time) bool {
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	classDateLoc := classDate.In(loc)

	cancelDeadline := time.Date(
		classDateLoc.Year(), classDateLoc.Month(), classDateLoc.Day()-1,
		23, 59, 0, 0, loc,
	)
	return now.Before(cancelDeadline)
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

// utils/misc_funcs.go
func GetAppName() string {
	if name := os.Getenv("APP_NAME"); name != "" {
		return name
	}
	return "MDX"
}
