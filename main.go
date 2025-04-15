package main

import (
	"fmt"
	"net/http"
	"strings"

	handlers "raft3d/api"
	"raft3d/store"
)

func main() {
	s := store.NewStore()
	h := handlers.NewHandler(s)

	http.HandleFunc("/api/v1/printers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.CreatePrinter(w, r)
		case http.MethodGet:
			h.GetPrinters(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/api/v1/filaments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.CreateFilament(w, r)
		case http.MethodGet:
			h.GetFilaments(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/api/v1/print_jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.CreatePrintJob(w, r)
		case http.MethodGet:
			h.GetPrintJobs(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/api/v1/print_jobs/", func(w http.ResponseWriter, r *http.Request) {
		// Expecting path: /api/v1/print_jobs/{id}/status
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Extract job ID
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 6 {
			http.Error(w, "Invalid URL", http.StatusBadRequest)
			return
		}
		jobID := parts[4]
		status := r.URL.Query().Get("status")
		if status == "" {
			http.Error(w, "Missing status parameter", http.StatusBadRequest)
			return
		}
		h.UpdatePrintJobStatus(w, r, jobID, status)
	})

	fmt.Println("Server running on http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
