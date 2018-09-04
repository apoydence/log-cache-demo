package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func main() {
	log.Println("Starting RaaS...")
	defer log.Println("Closing RaaS...")
	rand.Seed(time.Now().UnixNano())

	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(`{"value":%d}`, moreRandomy())))
	})))
}

func moreRandomy() int64 {
	c := make(chan int64)
	for i := 0; i < 100; i++ {
		go func() {
			for {
				timer := time.NewTimer(time.Millisecond)
				select {
				case c <- rand.Int63():
					return
				case <-timer.C:
					continue
				}
			}
		}()
	}

	var result int64
	for i := 0; i < 10; i++ {
		result += <-c
	}
	return result
}
