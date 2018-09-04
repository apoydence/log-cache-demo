package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	envstruct "code.cloudfoundry.org/go-envstruct"
	logcache "code.cloudfoundry.org/go-log-cache"
	"code.cloudfoundry.org/go-log-cache/rpc/logcache_v1"
	"code.cloudfoundry.org/go-loggregator/rpc/loggregator_v2"
)

func main() {
	log := log.New(os.Stderr, "", 0)
	cfg := LoadConfig(log)

	client := logcache.NewClient(
		cfg.LogCacheAddr,
		logcache.WithHTTPClient(
			addTokenDoer{cfg.Token},
		),
	)

	mg := map[string]float64{}
	mc := map[string]uint64{}
	mt := map[string]int64{}
	visitor := func(es []*loggregator_v2.Envelope) bool {
		for _, e := range es {
			switch e.Message.(type) {
			case *loggregator_v2.Envelope_Gauge:
				for name, value := range e.GetGauge().GetMetrics() {
					mg[name] = value.GetValue()
				}
			case *loggregator_v2.Envelope_Counter:
				mc[e.GetCounter().GetName()] = e.GetCounter().GetTotal()
			case *loggregator_v2.Envelope_Timer:
				timer := e.GetTimer()
				mt[timer.GetName()] = timer.GetStop() - timer.GetStart()
			}
		}

		return true
	}

	println("ASDF")
	now := time.Now()
	logcache.Walk(
		context.Background(),
		cfg.SourceID,
		visitor,
		client.Read,
		logcache.WithWalkStartTime(now.Add(-cfg.Interval)),
		logcache.WithWalkEndTime(now),
		logcache.WithWalkEnvelopeTypes(
			logcache_v1.EnvelopeType_COUNTER,
			logcache_v1.EnvelopeType_GAUGE,
			logcache_v1.EnvelopeType_TIMER,
		),
	)
	println("ASDF")

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 8, 2, '\t', 0)
	for name, value := range mg {
		fmt.Fprintln(w, fmt.Sprintf("%s\t%f\tGAUGE", name, value))
	}

	for name, value := range mc {
		fmt.Fprintln(w, fmt.Sprintf("%s\t%d\tCOUNTER", name, value))
	}

	for name, value := range mt {
		fmt.Fprintln(w, fmt.Sprintf("%s\t%s\tTIMER", name, time.Duration(value)))
	}
	w.Flush()
}

type Config struct {
	Token        string        `env:"TOKEN,required"`
	LogCacheAddr string        `env:"LOG_CACHE_ADDR,required"`
	SourceID     string        `env:"SOURCE_ID,required"`
	Interval     time.Duration `env:"INTERVAL"`
}

func LoadConfig(log *log.Logger) Config {
	cfg := Config{
		Interval: time.Minute,
	}
	if err := envstruct.Load(&cfg); err != nil {
		log.Fatal(err)
	}

	return cfg
}

type addTokenDoer struct {
	token string
}

func (d addTokenDoer) Do(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", d.token)
	return http.DefaultClient.Do(r)
}
