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
	faaspromql "github.com/poy/cf-faas-log-cache"
	gocapi "github.com/poy/go-capi"
)

func main() {
	log := log.New(os.Stderr, "[RESTARTER] ", log.LstdFlags)
	cfg := LoadConfig(log)
	capiClient := gocapi.NewClient(
		cfg.VcapApplication.CAPIAddr,
		cfg.VcapApplication.ApplicationID,
		cfg.VcapApplication.SpaceID,
		skipCacheDoer{},
	)

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	appGuid, err := capiClient.GetAppGuid(ctx, cfg.AppToRestart)
	if err != nil {
		log.Fatalf("failed to resolve app guid for %s: %s", cfg.AppToRestart, err)
	}
	log.Printf("resolved %s to %s", cfg.AppToRestart, appGuid)

	faaspromql.Start(faaspromql.HandlerFunc(func(r faaspromql.QueryResult) error {
		log.Printf("request to restart: %s", r.Context)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		lastEvent, err := capiClient.LastEvent(ctx, appGuid)
		if err != nil {
			return fmt.Errorf("failed to fetch last event for %s: %s", cfg.AppToRestart, err)
		}

		if len(lastEvent.Resources) > 0 &&
			lastEvent.Resources[0].Entity.Type == "audit.app.restart" &&
			lastEvent.Resources[0].MetaData.CreatedAt.After(time.Now().Add(-30*time.Second)) {
			log.Printf("skipping request to restart. We already restarted recently")
			return nil
		}

		log.Printf("restarting app %s...", cfg.AppToRestart)
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := capiClient.Restart(ctx, appGuid); err != nil {
			return fmt.Errorf("failed to restart %s: %s", cfg.AppToRestart, err)
		}

		return nil
	}))
}

type Config struct {
	AppToRestart    string          `env:"APP_TO_RESTART, required"`
	VcapApplication VcapApplication `env:"VCAP_APPLICATION, required"`
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
	cfg := Config{}
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
