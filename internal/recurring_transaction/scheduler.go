package recurring_transaction

import (
	"log"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	c   *cron.Cron
	svc *Service
}

func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{
		c:   cron.New(),
		svc: svc,
	}
}

func (s *Scheduler) Start() {
	s.c.AddFunc("0 0 * * *", func() {
		log.Println("Running recurring transaction scheduler...")
		if err := s.svc.ProcessDueRecurringTransactions(); err != nil {
			log.Printf("Recurring transaction scheduler error: %v", err)
		}
	})
	s.c.Start()
	log.Println("Recurring transaction scheduler started (daily at midnight)")
}

func (s *Scheduler) Stop() {
	s.c.Stop()
}
