package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/alfred-landrum/docker-metrics-plugin-test/reporter"
	"github.com/docker/go-plugins-helpers/sdk"
)

var (
	rep  *reporter.Reporter
	once sync.Once
)

func main() {
	h := sdk.NewHandler(`{"Implements": ["MetricsCollector"]}`)
	handlers(&h)
	if err := h.ServeUnix("metrics", 0); err != nil {
		panic(err)
	}
}

func handlers(h *sdk.Handler) {
	h.HandleFunc("/MetricsCollector.StartMetrics", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			rep = &reporter.Reporter{}
			rep.Start()
		})

		var res struct{ Err string }
		json.NewEncoder(w).Encode(&res)
	})

	h.HandleFunc("/MetricsCollector.StopMetrics", func(w http.ResponseWriter, r *http.Request) {
		rep.Stop()
		json.NewEncoder(w).Encode(map[string]string{})
	})
}
