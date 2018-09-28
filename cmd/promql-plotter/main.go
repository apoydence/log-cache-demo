package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	envstruct "code.cloudfoundry.org/go-envstruct"
	"github.com/apoydence/cf-faas-log-cache"
	"github.com/apoydence/cf-faas-log-cache/pkg/promql"
	gocapi "github.com/apoydence/go-capi"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
)

func main() {
	log := log.New(os.Stderr, "", 0)
	cfg := LoadConfig(log)

	capiClient := gocapi.NewClient(
		cfg.VcapApplication.CAPIAddr,
		"",
		cfg.VcapApplication.SpaceID,
		http.DefaultClient,
	)

	sanitizer := promql.NewSanitizer(capiClient)

	logCacheClient := promql.NewClient(
		cfg.VcapApplication.LogCacheAddr,
		sanitizer,
		http.DefaultClient,
	)

	http.HandleFunc("/", drawChart(os.Args[1:], logCacheClient, cfg, log))
	http.HandleFunc("/favicon.ico", func(res http.ResponseWriter, req *http.Request) {
		res.Write([]byte{})
	})
	http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil)
}

var colors = []drawing.Color{
	chart.ColorBlue,
	chart.ColorRed,
	chart.ColorGreen,
	chart.ColorOrange,
	chart.ColorBlack,
}

var colorMap = map[drawing.Color]int{
	chart.ColorBlue:   0,
	chart.ColorRed:    1,
	chart.ColorGreen:  2,
	chart.ColorOrange: 3,
	chart.ColorBlack:  4,
}

func createQuery(u *url.URL) (string, error) {
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", err
	}

	if len(values["source_id"]) != 1 {
		return "", errors.New("requires a single soursce_id")
	}

	if len(values["metric"]) != 1 {
		return "", errors.New("requires a single metric")
	}

	sourceID, metric := resolveSourceID(values.Get("source_id")), values.Get("metric")

	return fmt.Sprintf(`%s{source_id="%s"}`, metric, sourceID), nil
}

func resolveSourceID(sourceID string) string {
	return sourceID
}

func drawChart(
	queries []string,
	c *promql.Client,
	cfg Config,
	log *log.Logger,
) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		queries := queries

		if len(queries) == 0 {
			q, err := createQuery(req.URL)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(err.Error()))
				return
			}
			queries = []string{q}
		}

		now := time.Now()

		results := make(chan struct {
			result *faaspromql.QueryResult
			color  drawing.Color
		}, len(queries))

		errs := make(chan error, len(queries))

		var scalars []float64
		for i, query := range queries {
			if f, err := strconv.ParseFloat(query, 64); err == nil {
				scalars = append(scalars, f)
				continue
			}

			go func(query string, color drawing.Color) {
				result, err := c.PromQLRange(req.Context(), query, now.Add(-5*time.Minute), now, time.Second)
				if err != nil {
					errs <- fmt.Errorf("failed to make request: %s", err)
					return
				}

				if result.Status != "success" {
					errs <- fmt.Errorf("failed to make request (invalid status): %s", result.Status)
					return
				}

				if result.Data.ResultType != "matrix" {
					errs <- errors.New("query must yield a matrix")
					return
				}
				results <- struct {
					result *faaspromql.QueryResult
					color  drawing.Color
				}{result, color}
			}(query, colors[i%len(colors)])
		}

		var series []chart.Series
		for i := 0; i < len(queries)-len(scalars); i++ {
			select {
			case err := <-errs:
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				log.Println(err.Error())
				return
			case result := <-results:
				var (
					// xValues []time.Time
					xValues []float64
					yValues []float64
				)

				for _, p := range result.result.Data.Result {
					s := p.(*faaspromql.Series)
					for _, v := range s.Values {
						t, err := v[0].Float64()
						if err != nil {
							log.Panic(err)
						}

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

				style := chart.Style{
					Show:        true,
					StrokeColor: result.color,
					FillColor:   result.color.WithAlpha(100),
				}

				if cfg.Scatter {
					style = chart.Style{
						Show:        true,
						StrokeWidth: chart.Disabled,
						StrokeColor: result.color,
						DotWidth:    5,
					}
				}

				series = append(series, chart.ContinuousSeries{
					Style:   style,
					XValues: xValues,
					YValues: yValues,
				})
			}
		}

		if len(series) == 0 {
			log.Println("there has to be atleast one series")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("there has to be atleast one series"))
			return
		}

		sort.Sort(seriesColors(series))

		for i, scalar := range scalars {
			var cs chart.ContinuousSeries
			for _, x := range series[0].(chart.ContinuousSeries).XValues {
				cs.XValues = append(cs.XValues, x)
				cs.YValues = append(cs.YValues, scalar)
			}
			color := colors[(len(series)+i)%len(colors)]
			cs.Style = chart.Style{
				Show:        true,
				StrokeColor: color,
				FillColor:   color.WithAlpha(100),
			}
			series = append(series, cs)
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
			Series: series,
		}

		w.Header().Set("Content-Type", "image/png")
		graph.Render(chart.PNG, w)
	}
}

type Config struct {
	// This is not used, however the CF-Space-Security proxy needs it
	HttpProxy string `env:"HTTP_PROXY, required"`

	Port    int  `env:"PORT"`
	Scatter bool `env:"SCATTER_PLOT"`

	VcapApplication VcapApplication `env:"VCAP_APPLICATION, required"`
}

type VcapApplication struct {
	CAPIAddr        string `json:"cf_api"`
	LogCacheAddr    string
	ApplicationID   string   `json:"application_id"`
	ApplicationName string   `json:"application_name"`
	SpaceID         string   `json:"space_id"`
	ApplicationURIs []string `json:"application_uris"`
}

func (a *VcapApplication) UnmarshalEnv(data string) error {
	return json.Unmarshal([]byte(data), a)
}

func LoadConfig(log *log.Logger) Config {
	cfg := Config{
		Port: 8080,
	}

	if err := envstruct.Load(&cfg); err != nil {
		log.Fatal(err)
	}

	// Use HTTP so we can use HTTP_PROXY
	cfg.VcapApplication.CAPIAddr = strings.Replace(cfg.VcapApplication.CAPIAddr, "https", "http", 1)
	cfg.VcapApplication.LogCacheAddr = strings.Replace(cfg.VcapApplication.CAPIAddr, "api", "log-cache", 1)

	return cfg
}

type xyPair struct {
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

type seriesColors []chart.Series

func (c seriesColors) Len() int {
	return len(c)
}

func (c seriesColors) Swap(i, j int) {
	tmp := c[i]
	c[i] = c[j]
	c[j] = tmp
}

func (c seriesColors) Less(i, j int) bool {
	return colorMap[c[i].(chart.ContinuousSeries).Style.StrokeColor] < colorMap[c[j].(chart.ContinuousSeries).Style.StrokeColor]
}
