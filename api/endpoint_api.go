package api

import (
	"encoding/json"
	"net/http"

	"raft3d/models"
	"raft3d/store"
)

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{
		store: s,
	}
}

func (h *Handler) HandleNotLeader(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTemporaryRedirect)

	leader := h.store.GetLeader()
	response := map[string]string{
		"error":  "not the leader",
		"leader": leader,
	}
	json.NewEncoder(w).Encode(response)
}

// CreatePrinter handles POST /api/v1/printers
func (h *Handler) CreatePrinter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var printer models.Printer
	err := json.NewDecoder(r.Body).Decode(&printer)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if printer.ID == "" {
		http.Error(w, "printer id is required", http.StatusBadRequest)
		return
	}

	err = h.store.AddPrinter(printer)
	if err != nil {
		if err.Error() == "not the leader" {
			http.Error(w, "not the leader", http.StatusPreconditionFailed)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// GetPrinters handles GET /api/v1/printers
func (h *Handler) GetPrinters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	printers := h.store.GetPrinters()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(printers)
}

// CreateFilament handles POST /api/v1/filaments
func (h *Handler) CreateFilament(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var filament models.Filament
	err := json.NewDecoder(r.Body).Decode(&filament)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if filament.ID == "" {
		http.Error(w, "filament id is required", http.StatusBadRequest)
		return
	}

	err = h.store.AddFilament(filament)
	if err != nil {
		if err.Error() == "not the leader" {
			http.Error(w, "not the leader", http.StatusPreconditionFailed)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// GetFilaments handles GET /api/v1/filaments
func (h *Handler) GetFilaments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filaments := h.store.GetFilaments()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filaments)
}

// CreatePrintJob handles POST /api/v1/print_jobs
func (h *Handler) CreatePrintJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var printJob models.PrintJob
	err := json.NewDecoder(r.Body).Decode(&printJob)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if printJob.ID == "" {
		http.Error(w, "print job id is required", http.StatusBadRequest)
		return
	}

	if printJob.PrinterID == "" {
		http.Error(w, "printer id is required", http.StatusBadRequest)
		return
	}

	if printJob.FilamentID == "" {
		http.Error(w, "filament id is required", http.StatusBadRequest)
		return
	}

	err = h.store.AddPrintJob(printJob)
	if err != nil {
		if err.Error() == "not the leader" {
			http.Error(w, "not the leader", http.StatusPreconditionFailed)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// GetPrintJobs handles GET /api/v1/print_jobs
func (h *Handler) GetPrintJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	printJobs := h.store.GetPrintJobs()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(printJobs)
}

// UpdatePrintJobStatus handles POST /api/v1/print_jobs/{id}/status
func (h *Handler) UpdatePrintJobStatus(w http.ResponseWriter, r *http.Request, jobID, status string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := h.store.UpdatePrintJobStatus(jobID, status)
	if err != nil {
		if err.Error() == "not the leader" {
			http.Error(w, "not the leader", http.StatusPreconditionFailed)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
