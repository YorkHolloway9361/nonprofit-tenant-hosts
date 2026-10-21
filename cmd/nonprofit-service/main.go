package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/example/nonprofit-tenant-hosts/infrai"
	"github.com/example/nonprofit-tenant-hosts/nonprofit"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	pipeline := nonprofit.Pipeline{
		Infrai: infrai.New(key), ParentDomain: requiredEnv("PARENT_DOMAIN"),
		AssetOrigin: requiredEnv("ASSET_ORIGIN"), Bucket: envOr("ASSET_BUCKET", "nonprofit-assets"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /tenants", func(w http.ResponseWriter, r *http.Request) {
		var input nonprofit.TenantInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON input"})
			return
		}
		output, err := pipeline.Provision(r.Context(), input)
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *infrai.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
				status = apiErr.StatusCode
			}
			if strings.Contains(err.Error(), "are required") {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, output)
	})

	addr := envOr("LISTEN_ADDR", ":8080")
	log.Printf("nonprofit tenant service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
