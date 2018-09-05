package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	prism "github.com/pivotal-cf/go-prism"
)

func main() {
	plogger := prism.New()

	go func() {
		for range time.Tick(5 * time.Second) {
			plogger.LogGauge("go_routines", float64(runtime.NumGoroutine()), nil)
		}
	}()

	http.ListenAndServe(":"+os.Getenv("PORT"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := random()
		w.Write([]byte(fmt.Sprintf(`{"value":%d}`, value)))
	}))
}
