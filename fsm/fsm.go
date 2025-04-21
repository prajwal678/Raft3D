package fsm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"raft3d/models"

	"github.com/hashicorp/raft"
)

const (
	CommandAddPrinter           = "add_printer"
	CommandAddFilament          = "add_filament"
	CommandAddPrintJob          = "add_print_job"
	CommandUpdatePrintJobStatus = "update_print_job_status"
)

var (
	ErrPrinterNotFound   = errors.New("printer not found")
	ErrFilamentNotFound  = errors.New("filament not found")
	ErrPrintJobNotFound  = errors.New("print job not found")
	ErrInvalidTransition = errors.New("invalid status transition")
)

type Command struct {
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data,omitempty"`
	JobID  string          `json:"job_id,omitempty"`
	Status string          `json:"status,omitempty"`
}

type PrinterFSM struct {
	mu        sync.RWMutex
	printers  map[string]models.Printer
	filaments map[string]models.Filament
	printJobs map[string]models.PrintJob
}

func NewPrinterFSM() *PrinterFSM {
	return &PrinterFSM{
		printers:  make(map[string]models.Printer),
		filaments: make(map[string]models.Filament),
		printJobs: make(map[string]models.PrintJob),
	}
}

func (f *PrinterFSM) Apply(log *raft.Log) interface{} {
	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal command: %s", err)
	}

	switch cmd.Type {
	case CommandAddPrinter:
		return f.applyAddPrinter(cmd.Data)
	case CommandAddFilament:
		return f.applyAddFilament(cmd.Data)
	case CommandAddPrintJob:
		return f.applyAddPrintJob(cmd.Data)
	case CommandUpdatePrintJobStatus:
		return f.applyUpdatePrintJobStatus(cmd.JobID, cmd.Status)
	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

func (f *PrinterFSM) applyAddPrinter(data []byte) interface{} {
	var printer models.Printer
	if err := json.Unmarshal(data, &printer); err != nil {
		return fmt.Errorf("failed to unmarshal printer: %s", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.printers[printer.ID] = printer
	return nil
}

func (f *PrinterFSM) applyAddFilament(data []byte) interface{} {
	var filament models.Filament
	if err := json.Unmarshal(data, &filament); err != nil {
		return fmt.Errorf("failed to unmarshal filament: %s", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.filaments[filament.ID] = filament
	return nil
}

func (f *PrinterFSM) applyAddPrintJob(data []byte) interface{} {
	var printJob models.PrintJob
	if err := json.Unmarshal(data, &printJob); err != nil {
		return fmt.Errorf("failed to unmarshal print job: %s", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Set initial status to Queued if not set
	if printJob.Status == "" {
		printJob.Status = "Queued"
	}

	f.printJobs[printJob.ID] = printJob
	return nil
}

func ValidTransition(current, next string) bool {
	switch current {
	case "Queued":
		return next == "Running" || next == "Cancelled"
	case "Running":
		return next == "Done" || next == "Cancelled"
	default:
		return false
	}
}

func (f *PrinterFSM) applyUpdatePrintJobStatus(jobID, status string) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	printJob, exists := f.printJobs[jobID]
	if !exists {
		return fmt.Errorf("print job not found: %s", jobID)
	}

	if !ValidTransition(printJob.Status, status) {
		return ErrInvalidTransition
	}

	if status == "Done" && printJob.Status == "Running" {
		filament, ok := f.filaments[printJob.FilamentID]
		if !ok {
			return ErrFilamentNotFound
		}

		if filament.RemainingWeightInGrams >= printJob.PrintWeightInGrams {
			filament.RemainingWeightInGrams -= printJob.PrintWeightInGrams
		} else {
			filament.RemainingWeightInGrams = 0 // Safety check
		}

		f.filaments[printJob.FilamentID] = filament
	}

	printJob.Status = status
	f.printJobs[jobID] = printJob
	return nil
}

func (f *PrinterFSM) GetPrinters() map[string]models.Printer {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make(map[string]models.Printer, len(f.printers))
	for k, v := range f.printers {
		result[k] = v
	}
	return result
}

func (f *PrinterFSM) GetFilaments() map[string]models.Filament {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make(map[string]models.Filament, len(f.filaments))
	for k, v := range f.filaments {
		result[k] = v
	}
	return result
}

func (f *PrinterFSM) GetPrintJobs() map[string]models.PrintJob {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make(map[string]models.PrintJob, len(f.printJobs))
	for k, v := range f.printJobs {
		result[k] = v
	}
	return result
}

func (f *PrinterFSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	printers := make(map[string]models.Printer, len(f.printers))
	for k, v := range f.printers {
		printers[k] = v
	}

	filaments := make(map[string]models.Filament, len(f.filaments))
	for k, v := range f.filaments {
		filaments[k] = v
	}

	printJobs := make(map[string]models.PrintJob, len(f.printJobs))
	for k, v := range f.printJobs {
		printJobs[k] = v
	}

	return &PrinterSnapshot{
		Printers:  printers,
		Filaments: filaments,
		PrintJobs: printJobs,
	}, nil
}

func (f *PrinterFSM) Restore(rc io.ReadCloser) error {
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	var snapshot PrinterSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.printers = snapshot.Printers
	f.filaments = snapshot.Filaments
	f.printJobs = snapshot.PrintJobs

	return nil
}

type PrinterSnapshot struct {
	Printers  map[string]models.Printer
	Filaments map[string]models.Filament
	PrintJobs map[string]models.PrintJob
}

func (s *PrinterSnapshot) Persist(sink raft.SnapshotSink) error {
	data, err := json.Marshal(s)
	if err != nil {
		sink.Cancel()
		return err
	}

	if _, err := sink.Write(data); err != nil {
		sink.Cancel()
		return err
	}

	return sink.Close()
}

func (s *PrinterSnapshot) Release() {}
