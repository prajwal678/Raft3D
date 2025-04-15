package handlers

import (
	"encoding/json"
	"net/http"
	"raft3d/models"
	"raft3d/store"
	"raft3d/utils"
)

// Handler struct that includes reference to the in-memory store
type Handler struct {
	store *store.Store
}

// NewHandler returns a new Handler instance
func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

// CreatePrinter handles POST /api/v1/printers
func (h *Handler) CreatePrinter(w http.ResponseWriter, r *http.Request) {
	var printer models.Printer
	if err := json.NewDecoder(r.Body).Decode(&printer); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.store.AddPrinter(printer)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(printer)
}

// GetPrinters handles GET /api/v1/printers
func (h *Handler) GetPrinters(w http.ResponseWriter, r *http.Request) {
	printers := h.store.GetPrinters()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(printers)
}

// CreateFilament handles POST /api/v1/filaments
func (h *Handler) CreateFilament(w http.ResponseWriter, r *http.Request) {
	var filament models.Filament
	if err := json.NewDecoder(r.Body).Decode(&filament); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.store.AddFilament(filament)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(filament)
}

// GetFilaments handles GET /api/v1/filaments
func (h *Handler) GetFilaments(w http.ResponseWriter, r *http.Request) {
	filaments := h.store.GetFilaments()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filaments)
}

// CreatePrintJob handles POST /api/v1/print_jobs
func (h *Handler) CreatePrintJob(w http.ResponseWriter, r *http.Request) {
	var printJob models.PrintJob
	if err := json.NewDecoder(r.Body).Decode(&printJob); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate the printer and filament existence
	_, printerExists := h.store.Printers[printJob.PrinterID]
	if !printerExists {
		http.Error(w, "Printer not found", http.StatusBadRequest)
		return
	}

	filament, filamentExists := h.store.Filaments[printJob.FilamentID]
	if !filamentExists {
		http.Error(w, "Filament not found", http.StatusBadRequest)
		return
	}

	// Check filament availability
	remainingWeight := filament.RemainingWeightInGrams
	for _, job := range h.store.PrintJobs {
		if job.FilamentID == printJob.FilamentID && job.Status == "Queued" {
			remainingWeight -= job.PrintWeightInGrams
		}
	}

	if printJob.PrintWeightInGrams > remainingWeight {
		http.Error(w, "Not enough filament remaining", http.StatusBadRequest)
		return
	}

	// Add the print job to the store
	h.store.AddPrintJob(printJob)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(printJob)
}

// GetPrintJobs handles GET /api/v1/print_jobs
func (h *Handler) GetPrintJobs(w http.ResponseWriter, r *http.Request) {
	printJobs := h.store.GetPrintJobs()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(printJobs)
}

// UpdatePrintJobStatus handles POST /api/v1/print_jobs/{id}/status
func (h *Handler) UpdatePrintJobStatus(w http.ResponseWriter, r *http.Request, jobID string, newStatus string) {
	job, exists := h.store.PrintJobs[jobID]
	if !exists {
		http.Error(w, "Print job not found", http.StatusNotFound)
		return
	}

	if !utils.ValidTransition(job.Status, newStatus) {
		http.Error(w, "Invalid status transition", http.StatusBadRequest)
		return
	}

	// Perform transition
	if newStatus == "Done" {
		filament, ok := h.store.Filaments[job.FilamentID]
		if !ok {
			http.Error(w, "Associated filament not found", http.StatusInternalServerError)
			return
		}
		// Reduce filament weight
		filament.RemainingWeightInGrams -= job.PrintWeightInGrams
		h.store.Filaments[job.FilamentID] = filament
	}

	job.Status = newStatus
	h.store.PrintJobs[jobID] = job

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}
