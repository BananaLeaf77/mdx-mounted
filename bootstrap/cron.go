package bootstrap

import (
	"chronosphere/domain"
	"context"
	"log"
	"time"

	"github.com/robfig/cron/v3"
)

func InitCron(teacherPaymentService domain.TeacherPaymentUseCase) *cron.Cron {
	log.Println("⏰ Initializing Cron Jobs...")

	c := cron.New(cron.WithLocation(time.Local))

	// Every 1st of the month at 00:00 (midnight)
	// Cron expression: minute hour dom month dow
	_, err := c.AddFunc("0 0 1 * *", func() {
		log.Println("🚀 [CRON] Starting GenerateMonthlyPayments for the previous month...")
		
		loc, _ := time.LoadLocation("Asia/Makassar")
		now := time.Now().In(loc)
		
		// The payment to generate is for the PREVIOUS month
		// If it's currently March 1st, we generate payments for February.
		targetMonth := now.AddDate(0, -1, 0)
		
		year := targetMonth.Year()
		month := int(targetMonth.Month())
		
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		details, err := teacherPaymentService.GenerateMonthlyPayments(ctx, year, month)
		if err != nil {
			log.Printf("❌ [CRON] Failed to GenerateMonthlyPayments for %d-%02d: %v", year, month, err)
			return
		}
		
		log.Printf("✅ [CRON] Successfully generated monthly payments for %d-%02d. Total records processed: %d", year, month, len(details))
	})

	if err != nil {
		log.Fatalf("❌ Failed to initialize cron jobs: %v", err)
	}

	c.Start()
	log.Println("✅ Cron Jobs started.")
	
	return c
}
