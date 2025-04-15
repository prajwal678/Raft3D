package store

import (
	"errors"
	"sync"

	"raft3d/models"
)

type Store struct {
	Printers  map[string]models.Printer
	Filaments map[string]models.Filament
	PrintJobs map[string]models.PrintJob
	mu        sync.RWMutex
}

var (
	ErrPrinterNotFound  = errors.New("printer not found")
	ErrFilamentNotFound = errors.New("filament not found")
	ErrJobNotFound      = errors.New("print job not found")
)

func NewStore() *Store {
	return &Store{
		Printers:  make(map[string]models.Printer),
		Filaments: make(map[string]models.Filament),
		PrintJobs: make(map[string]models.PrintJob),
	}
}

// AddPrinter adds a new printer to the store
func (s *Store) AddPrinter(printer models.Printer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Printers[printer.ID] = printer
}

// GetPrinters returns all printers
func (s *Store) GetPrinters() []models.Printer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	printers := make([]models.Printer, 0, len(s.Printers))
	for _, p := range s.Printers {
		printers = append(printers, p)
	}
	return printers
}

// AddFilament adds a new filament to the store
func (s *Store) AddFilament(filament models.Filament) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Filaments[filament.ID] = filament
}

// GetFilaments returns all filaments
func (s *Store) GetFilaments() []models.Filament {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filaments := make([]models.Filament, 0, len(s.Filaments))
	for _, f := range s.Filaments {
		filaments = append(filaments, f)
	}
	return filaments
}

// AddPrintJob adds a new print job to the store
func (s *Store) AddPrintJob(job models.PrintJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PrintJobs[job.ID] = job
}

// GetPrintJobs returns all print jobs
func (s *Store) GetPrintJobs() []models.PrintJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]models.PrintJob, 0, len(s.PrintJobs))
	for _, j := range s.PrintJobs {
		jobs = append(jobs, j)
	}
	return jobs
}

// UpdatePrintJobStatus updates the status of a print job
func (s *Store) UpdatePrintJobStatus(jobID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, exists := s.PrintJobs[jobID]
	if !exists {
		return ErrJobNotFound
	}
	job.Status = status
	s.PrintJobs[jobID] = job
	return nil
}
