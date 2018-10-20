package main

import (
	"fmt"
	"log"
	"os"

	"github.com/poy/cf-faas-log-cache/pkg/promql"
)

func main() {
	log := log.New(os.Stderr, "", 0)

	if len(os.Args) != 2 {
		log.Fatalf("usage: %s [Prometheus Query]", os.Args[0])
	}

	sids, err := promql.Parse(os.Args[1])
	if err != nil {
		log.Fatal(err.Error())
	}

	for _, sid := range sids {
		fmt.Println(sid)
	}
}
