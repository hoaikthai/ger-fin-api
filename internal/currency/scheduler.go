package currency

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
	s.c.AddFunc("0 * * * *", func() {
		log.Println("currency: refreshing exchange rates...")
		if err := s.svc.FetchAndStoreRates(); err != nil {
			log.Printf("currency: rate refresh error: %v", err)
		}
	})
	s.c.Start()
	log.Println("currency: rate refresh scheduler started (hourly)")
}

func (s *Scheduler) Stop() {
	s.c.Stop()
}
