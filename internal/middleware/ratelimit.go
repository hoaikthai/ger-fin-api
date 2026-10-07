package middleware

import (
	"strconv"
	"sync"
	"time"

	"ger_fin_api/internal/common"

	"github.com/gin-gonic/gin"
)

type rateWindow struct {
	start time.Time
	count int
}

// RateLimit allows at most limit requests per client IP within each window
// (fixed window) and answers 429 beyond that. Intended for the unauthenticated
// /auth routes to slow down brute force and credential stuffing.
func RateLimit(limit int, window time.Duration) gin.HandlerFunc {
	var mu sync.Mutex
	clients := make(map[string]*rateWindow)
	lastSweep := time.Now()

	return func(c *gin.Context) {
		now := time.Now()
		ip := c.ClientIP()

		mu.Lock()
		// Drop expired entries periodically so the map stays bounded.
		if now.Sub(lastSweep) > window {
			for k, w := range clients {
				if now.Sub(w.start) >= window {
					delete(clients, k)
				}
			}
			lastSweep = now
		}
		w, ok := clients[ip]
		if !ok || now.Sub(w.start) >= window {
			w = &rateWindow{start: now}
			clients[ip] = w
		}
		w.count++
		exceeded := w.count > limit
		retryAfter := w.start.Add(window).Sub(now)
		mu.Unlock()

		if exceeded {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			common.RespondError(c, &common.AppError{Code: 429, Msg: "Too many requests, please try again later", ErrText: "Too Many Requests"})
			c.Abort()
			return
		}
		c.Next()
	}
}
