package budget

import (
	"log"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	c   *cron.Cron
	svc *Service
}

func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{c: cron.New(), svc: svc}
}

func (s *Scheduler) Start() {
	s.c.AddFunc("0 0 * * *", func() {
		log.Println("Running budget rollover scheduler...")
		if err := s.svc.ProcessDueBudgetRollovers(); err != nil {
			log.Printf("Budget rollover scheduler error: %v", err)
		}
	})
	s.c.Start()
	log.Println("Budget rollover scheduler started (daily at midnight)")
}

func (s *Scheduler) Stop() {
	s.c.Stop()
}
