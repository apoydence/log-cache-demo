package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	envstruct "code.cloudfoundry.org/go-envstruct"
	faaspromql "github.com/apoydence/cf-faas-log-cache"
	gocapi "github.com/apoydence/go-capi"
)

func main() {
	log := log.New(os.Stderr, "[SCALER] ", log.LstdFlags)
	cfg := LoadConfig(log)
	capiClient := gocapi.NewClient(
		cfg.VcapApplication.CAPIAddr,
		cfg.VcapApplication.ApplicationID,
		cfg.VcapApplication.SpaceID,
		skipCacheDoer{},
	)

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	appGuid, err := capiClient.GetAppGuid(ctx, cfg.AppToScale)
	if err != nil {
		log.Fatalf("failed to resolve app guid for %s: %s", cfg.AppToScale, err)
	}
	log.Printf("resolved %s to %s", cfg.AppToScale, appGuid)

	faaspromql.Start(faaspromql.HandlerFunc(func(r faaspromql.QueryResult) error {
		log.Printf("request to scale: %s", r.Context)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		lastEvent, err := capiClient.LastEvent(ctx, appGuid)
		if err != nil {
			return fmt.Errorf("failed to fetch last event for %s: %s", cfg.AppToScale, err)
		}

		if len(lastEvent.Resources) > 0 &&
			lastEvent.Resources[0].Entity.Type == "audit.app.update" &&
			lastEvent.Resources[0].MetaData.CreatedAt.After(time.Now().Add(-30*time.Second)) {
			log.Printf("skipping request to scale. We already scaled recently")
			return nil
		}

		ps, err := capiClient.ProcessStats(ctx, appGuid)
		if err != nil {
			return fmt.Errorf("failed to fetch process stats for %s: %s", cfg.AppToScale, err)
		}

		instanceCount, ok := scaleTo(len(ps), cfg)
		if !ok {
			log.Printf("scaling to %d would be beyond our threshold", instanceCount)
			return nil
		}

		log.Printf("scaling app %s to %d...", cfg.AppToScale, instanceCount)
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := capiClient.Scale(ctx, appGuid, instanceCount); err != nil {
			return fmt.Errorf("failed to scale %s to %d: %s", cfg.AppToScale, instanceCount, err)
		}

		return nil
	}))
}

func scaleTo(current int, cfg Config) (int, bool) {
	if cfg.ScaleUp {
		current++
		return current, current <= cfg.Max
	}

	current--
	return current, current >= cfg.Min
}

type Config struct {
	AppToScale      string          `env:"APP_TO_SCALE, required"`
	VcapApplication VcapApplication `env:"VCAP_APPLICATION, required"`

	ScaleUp bool `env:"SCALE_UP"`
	Max     int  `env:"MAX_INSTANCES"`
	Min     int  `env:"MIN_INSTANCES"`
}

type VcapApplication struct {
	CAPIAddr        string   `json:"cf_api"`
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
		Max: 5,
		Min: 1,
	}
	if err := envstruct.Load(&cfg); err != nil {
		log.Fatal(err)
	}

	// Use HTTP so we can use HTTP_PROXY
	cfg.VcapApplication.CAPIAddr = strings.Replace(cfg.VcapApplication.CAPIAddr, "https", "http", 1)

	return cfg
}

type skipCacheDoer struct{}

func (d skipCacheDoer) Do(r *http.Request) (*http.Response, error) {
	r.Header.Set("Cache-Control", "no-cache")
	return http.DefaultClient.Do(r)
}
