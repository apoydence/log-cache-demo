package main

import (
	"math/rand"
	"sync/atomic"
	"time"
)

func random() int64 {
	rateLimiter()
	c := make(chan int64)
	for i := 0; i < 100; i++ {
		go func() {
			c <- rand.Int63()
		}()
	}

	var result int64
	for i := 0; i < 10; i++ {
		result += <-c
	}
	return result
}

var count int64

func init() {
	resetCount()
}

func resetCount() {
	go func() {
		for range time.Tick(time.Second) {
			atomic.StoreInt64(&count, 0)
		}
	}()
}

func rateLimiter() {
	c := atomic.AddInt64(&count, 1)
	time.Sleep(20 * time.Duration(c) * time.Millisecond)
}
