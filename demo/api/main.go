// wtc-demo-api — dummy service for exercising the wtc change ledger.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

var version = "dev" // stamped via -ldflags at image build

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service": "api",
			"version": version,
			"pod":     os.Getenv("HOSTNAME"),
		})
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("wtc-demo-api %s listening on :8080", version)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
