package store

import (
	"errors"
	"sync"

	"raft3d/models"
	"raft3d/raftnode"
)

var ErrNotLeader = errors.New("not the leader")

type Store struct {
	raftNode *raftnode.RaftNode
	mu       sync.RWMutex
}

var (
	ErrPrinterNotFound  = errors.New("printer not found")
	ErrFilamentNotFound = errors.New("filament not found")
	ErrJobNotFound      = errors.New("print job not found")
)

func NewStore(node *raftnode.RaftNode) *Store {
	return &Store{
		raftNode: node,
	}
}

func (s *Store) AddPrinter(printer models.Printer) error {
	if err := s.raftNode.AddPrinter(printer); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetPrinters() map[string]models.Printer {
	printers, _, _ := s.raftNode.GetState()
	return printers
}

func (s *Store) GetPrinter(id string) (models.Printer, bool) {
	printers, _, _ := s.raftNode.GetState()
	printer, ok := printers[id]
	return printer, ok
}

func (s *Store) AddFilament(filament models.Filament) error {
	if err := s.raftNode.AddFilament(filament); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetFilaments() map[string]models.Filament {
	_, filaments, _ := s.raftNode.GetState()
	return filaments
}

func (s *Store) GetFilament(id string) (models.Filament, bool) {
	_, filaments, _ := s.raftNode.GetState()
	filament, ok := filaments[id]
	return filament, ok
}

func (s *Store) AddPrintJob(job models.PrintJob) error {
	if err := s.raftNode.AddPrintJob(job); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetPrintJobs() map[string]models.PrintJob {
	_, _, printJobs := s.raftNode.GetState()
	return printJobs
}

func (s *Store) GetPrintJob(id string) (models.PrintJob, bool) {
	_, _, printJobs := s.raftNode.GetState()
	job, ok := printJobs[id]
	return job, ok
}

func (s *Store) UpdatePrintJobStatus(id, status string) error {
	if err := s.raftNode.UpdatePrintJobStatus(id, status); err != nil {
		return err
	}
	return nil
}

func (s *Store) IsLeader() bool {
	return s.raftNode.IsLeader()
}

func (s *Store) GetLeader() string {
	return s.raftNode.GetLeader()
}

func (s *Store) AddServer(id, addr string) error {
	return s.raftNode.AddServer(id, addr)
}

func (s *Store) RemoveServer(id string) error {
	return s.raftNode.RemoveServer(id)
}

func (s *Store) GetClusterStats() map[string]interface{} {
	return s.raftNode.GetNodeStats()
}
