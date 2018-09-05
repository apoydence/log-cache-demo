package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"

	envstruct "code.cloudfoundry.org/go-envstruct"
	"github.com/apoydence/cf-faas-log-cache"
	"github.com/apoydence/cf-faas-log-cache/pkg/promql"
	gocapi "github.com/apoydence/go-capi"
	"github.com/wcharczuk/go-chart"
)

func main() {
	log := log.New(os.Stderr, "", 0)

	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s [PromQL Query]", os.Args[0])
	}
	query := os.Args[1]

	cfg := LoadConfig(log)

	capiClient := gocapi.NewClient(
		cfg.CAPIAddr,
		"",
		cfg.SpaceID,
		addTokenDoer{cfg.Token},
	)

	sanitizer := promql.NewSanitizer(capiClient)

	logCacheClient := promql.NewClient(
		cfg.LogCacheAddr,
		sanitizer,
		addTokenDoer{cfg.Token},
	)

	http.HandleFunc("/", drawChart(query, logCacheClient, cfg, log))
	http.HandleFunc("/favicon.ico", func(res http.ResponseWriter, req *http.Request) {
		res.Write([]byte{})
	})
	http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil)
}

func drawChart(query string, c *promql.Client, cfg Config, log *log.Logger) func(res http.ResponseWriter, req *http.Request) {
	return func(res http.ResponseWriter, req *http.Request) {
		result, err := c.PromQL(req.Context(), query)
		if err != nil {
			log.Printf("failed to make request: %s", err)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte("failed to make request"))
			return
		}

		if result.Status != "success" {
			log.Printf("failed to make request: invalid Status %s", result.Status)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte("failed to make request"))
			return
		}

		if result.Data.ResultType != "matrix" {
			log.Printf("query must yield a matrix")
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte("failed to make request"))
			return
		}

		var (
			// xValues []time.Time
			xValues []float64
			yValues []float64
		)

		for _, p := range result.Data.Result {
			s := p.(*faaspromql.Series)
			for _, v := range s.Values {
				i, err := v[0].Int64()
				if err != nil {
					log.Panic(err)
				}

				t := float64(i / 1e6)

				f, err := v[1].Float64()
				if err != nil {
					log.Panic(err)
				}

				xValues = append(xValues, t)
				yValues = append(yValues, f)
			}
		}

		sort.Sort(xyPair{xValues, yValues})

		for i := 1; i < len(xValues); i++ {
			for xValues[i-1] >= xValues[i] {
				xValues[i] += 50
			}
		}

		if cfg.PrintNumPoints {
			log.Printf("Number of Points: %d", len(xValues))
		}

		style := chart.Style{
			Show:        true,
			StrokeColor: chart.ColorBlue,
			FillColor:   chart.ColorBlue.WithAlpha(100),
		}

		if cfg.Scatter {
			style = chart.Style{
				Show:        true,
				StrokeWidth: chart.Disabled,
				DotWidth:    5,
			}
		}

		graph := chart.Chart{
			XAxis: chart.XAxis{
				Style: chart.Style{
					Show: true,
				},
			},
			YAxis: chart.YAxis{
				Style: chart.Style{
					Show: true,
				},
			},
			Series: []chart.Series{
				chart.ContinuousSeries{
					Style:   style,
					XValues: xValues,
					YValues: yValues,
				},
			},
		}

		res.Header().Set("Content-Type", "image/png")
		graph.Render(chart.PNG, res)
	}
}

type Config struct {
	Port           int    `env:"PORT"`
	LogCacheAddr   string `env:"LOG_CACHE_ADDR,required"`
	CAPIAddr       string `env:"CAPI_ADDR,required"`
	SpaceID        string `env:"SPACE_ID,required"`
	Token          string `env:"TOKEN,required"`
	Scatter        bool   `env:"SCATTER_PLOT"`
	PrintNumPoints bool   `env:"PRINT_NUM_POINTS"`
}

func LoadConfig(log *log.Logger) Config {
	cfg := Config{
		Port: 8080,
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

type xyPair struct {
	// xValues []time.Time
	xValues []float64
	yValues []float64
}

func (p xyPair) Len() int {
	return len(p.xValues)
}

func (p xyPair) Swap(i, j int) {
	tmpx := p.xValues[i]
	p.xValues[i] = p.xValues[j]
	p.xValues[j] = tmpx

	tmpy := p.yValues[i]
	p.yValues[i] = p.yValues[j]
	p.yValues[j] = tmpy
}

func (p xyPair) Less(i, j int) bool {
	// return p.xValues[i].UnixNano() < p.xValues[j].UnixNano()
	return p.xValues[i] < p.xValues[j]
}
