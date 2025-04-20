package raftnode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"raft3d/fsm"
	"raft3d/models"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// ServerStats contains statistics for a server in the cluster
type ServerStats struct {
	LastContact    time.Time     `json:"last_contact"`
	JoinTime       time.Time     `json:"join_time"`
	FailureCount   int           `json:"failure_count"`
	LeadershipTime time.Duration `json:"leadership_time,omitempty"`
	State          string        `json:"state"`
	Address        string        `json:"address"`
}

type RaftNode struct {
	nodeID      string
	raftAddr    string
	dataDir     string
	bootstrap   bool
	raft        *raft.Raft
	fsm         *fsm.PrinterFSM
	logStore    raft.LogStore
	stableStore raft.StableStore
	transport   raft.Transport

	failureDetector  map[string]int // Tracks failed contact attempts
	failureThreshold int            // Number of failures before removing a server
	monitorShutdown  chan struct{}
	lastContactTimes map[string]time.Time
	joinTimes        map[string]time.Time

	stats           map[string]*ServerStats
	statsMutex      sync.RWMutex
	leaderSince     time.Time
	totalLeaderTime time.Duration
	termTransitions int
	appliedCommands int64
}

func NewRaftNode(nodeID, raftAddr, dataDir string, bootstrap bool) (*RaftNode, error) {
	printerFSM := fsm.NewPrinterFSM()

	// boltDB suppressed warnings cuz annoying af
	oldLogOutput := log.Writer()
	log.SetOutput(ioutil.Discard)

	logStorePath := filepath.Join(dataDir, "raft.db")
	boltDB, err := raftboltdb.NewBoltStore(logStorePath)

	// restore normal logging
	log.SetOutput(oldLogOutput)

	if err != nil {
		return nil, fmt.Errorf("failed to create bolt store: %w", err)
	}

	logStore := boltDB
	stableStore := boltDB

	// Create snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(dataDir, 3, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create transport
	tcpAddr, err := net.ResolveTCPAddr("tcp", raftAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tcp address: %w", err)
	}

	transport, err := raft.NewTCPTransport(raftAddr, tcpAddr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create tcp transport: %w", err)
	}

	// Create Raft config
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(nodeID)

	// Tune Raft parameters for better election stability
	config.HeartbeatTimeout = 1500 * time.Millisecond   // Increase heartbeat timeout (default: 500-1000ms)
	config.ElectionTimeout = 2000 * time.Millisecond    // Increase election timeout (default: 1000-2000ms)
	config.CommitTimeout = 200 * time.Millisecond       // Time to compact and commit logs
	config.SnapshotInterval = 120 * time.Second         // More frequent snapshots
	config.SnapshotThreshold = 1024                     // Smaller threshold for more frequent snapshots
	config.LeaderLeaseTimeout = 1000 * time.Millisecond // Must be less than heartbeat timeout

	config.TrailingLogs = 10240  // more logs
	config.MaxAppendEntries = 64 // small batches for stability

	r, err := raft.NewRaft(config, printerFSM, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft node: %w", err)
	}

	node := &RaftNode{
		nodeID:           nodeID,
		raftAddr:         raftAddr,
		dataDir:          dataDir,
		bootstrap:        bootstrap,
		raft:             r,
		fsm:              printerFSM,
		logStore:         logStore,
		stableStore:      stableStore,
		transport:        transport,
		failureDetector:  make(map[string]int),
		failureThreshold: 20,
		monitorShutdown:  make(chan struct{}),
		lastContactTimes: make(map[string]time.Time),
		joinTimes:        make(map[string]time.Time),
		stats:            make(map[string]*ServerStats),
		termTransitions:  0,
	}

	go node.monitorLeadership()
	go node.monitorServerHealth()
	go node.collectStats()

	if bootstrap {
		log.Println("bootstrapping raft cluster")

		cfg := r.GetConfiguration()
		if err := cfg.Error(); err != nil {
			return nil, fmt.Errorf("failed to get raft configuration: %w", err)
		}

		if len(cfg.Configuration().Servers) == 0 {
			configuration := raft.Configuration{
				Servers: []raft.Server{
					{
						ID:       raft.ServerID(nodeID),
						Address:  raft.ServerAddress(raftAddr),
						Suffrage: raft.Voter,
					},
				},
			}

			f := r.BootstrapCluster(configuration)
			if err := f.Error(); err != nil {
				return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
			}
		} else {
			log.Println("node already bootstrapped, skipping")
		}
	}

	node.statsMutex.Lock()
	node.stats[nodeID] = &ServerStats{
		JoinTime: time.Now(),
		Address:  raftAddr,
		State:    node.getStateString(),
	}
	node.statsMutex.Unlock()

	return node, nil
}

func (n *RaftNode) AddServer(nodeID, raftAddr string) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	log.Printf("adding new server to raft cluster: %s at %s", nodeID, raftAddr)

	joinTime := time.Now()
	n.joinTimes[nodeID] = joinTime

	n.statsMutex.Lock()
	n.stats[nodeID] = &ServerStats{
		JoinTime: joinTime,
		Address:  raftAddr,
		State:    "joining",
	}
	n.statsMutex.Unlock()

	future := n.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(raftAddr), 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("error adding server to raft cluster: %w", err)
	}

	return nil
}

func (n *RaftNode) GetLeader() string {
	return string(n.raft.Leader())
}

func (n *RaftNode) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

func (n *RaftNode) GetState() (map[string]models.Printer, map[string]models.Filament, map[string]models.PrintJob) {
	return n.fsm.GetPrinters(), n.fsm.GetFilaments(), n.fsm.GetPrintJobs()
}

func (n *RaftNode) Shutdown() error {
	log.Printf("shutting down raft node %s", n.nodeID)

	close(n.monitorShutdown)

	future := n.raft.Shutdown()
	if err := future.Error(); err != nil {
		log.Printf("error shutting down raft: %v", err)
		return err
	}

	if t, ok := n.transport.(*raft.NetworkTransport); ok {
		t.Close()
	}

	if bs, ok := n.logStore.(*raftboltdb.BoltStore); ok {
		if err := bs.Close(); err != nil {
			log.Printf("error closing boltdb store: %v", err)
		}
	}

	syscall.Sync()

	log.Printf("raft node %s shutdown complete", n.nodeID)
	return nil
}

func (n *RaftNode) AddPrinter(printer models.Printer) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	data, err := json.Marshal(printer)
	if err != nil {
		return err
	}

	cmd := fsm.Command{
		Type: fsm.CommandAddPrinter,
		Data: json.RawMessage(data),
	}

	cmdData, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	f := n.raft.Apply(cmdData, 30*time.Second)
	if err := f.Error(); err != nil {
		return err
	}

	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	n.statsMutex.Lock()
	n.appliedCommands++
	n.statsMutex.Unlock()

	return nil
}

func (n *RaftNode) AddFilament(filament models.Filament) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	data, err := json.Marshal(filament)
	if err != nil {
		return err
	}

	cmd := fsm.Command{
		Type: fsm.CommandAddFilament,
		Data: json.RawMessage(data),
	}

	cmdData, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	f := n.raft.Apply(cmdData, 30*time.Second)
	if err := f.Error(); err != nil {
		return err
	}

	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	n.statsMutex.Lock()
	n.appliedCommands++
	n.statsMutex.Unlock()

	return nil
}

func (n *RaftNode) AddPrintJob(job models.PrintJob) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	data, err := json.Marshal(job)
	if err != nil {
		return err
	}

	cmd := fsm.Command{
		Type: fsm.CommandAddPrintJob,
		Data: json.RawMessage(data),
	}

	cmdData, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	f := n.raft.Apply(cmdData, 30*time.Second)
	if err := f.Error(); err != nil {
		return err
	}

	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	n.statsMutex.Lock()
	n.appliedCommands++
	n.statsMutex.Unlock()

	return nil
}

func (n *RaftNode) UpdatePrintJobStatus(jobID, status string) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	cmd := fsm.Command{
		Type:   fsm.CommandUpdatePrintJobStatus,
		JobID:  jobID,
		Status: status,
	}

	cmdData, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	f := n.raft.Apply(cmdData, 30*time.Second)
	if err := f.Error(); err != nil {
		return err
	}

	if resp := f.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	n.statsMutex.Lock()
	n.appliedCommands++
	n.statsMutex.Unlock()

	return nil
}

func (n *RaftNode) RemoveServer(nodeID string) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	log.Printf("removing server %s from the cluster configuration", nodeID)
	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("error removing server: %w", err)
	}

	delete(n.failureDetector, nodeID)
	delete(n.lastContactTimes, nodeID)

	n.statsMutex.Lock()
	delete(n.stats, nodeID)
	n.statsMutex.Unlock()

	return nil
}

func (n *RaftNode) monitorServerHealth() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if n.raft.State() == raft.Leader {
				n.checkServers()
			}
		case <-n.monitorShutdown:
			return
		}
	}
}

func (n *RaftNode) checkServers() {
	configFuture := n.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		log.Printf("error getting raft configuration: %v", err)
		return
	}

	stats := n.raft.Stats()

	for _, server := range configFuture.Configuration().Servers {
		serverID := string(server.ID)

		if serverID == n.nodeID {
			continue
		}

		// Check last contact time from stats
		lastContactKey := fmt.Sprintf("last_contact_%s", serverID)
		lastContactStr, ok := stats[lastContactKey]

		// Update last contact time in stats
		n.statsMutex.Lock()
		if s, exists := n.stats[serverID]; exists {
			// Update the server state
			s.State = "voter" // Default state
			if server.Suffrage == raft.Nonvoter {
				s.State = "nonvoter"
			}
			s.Address = string(server.Address)
		} else {
			// If stats don't exist, create them
			n.stats[serverID] = &ServerStats{
				Address: string(server.Address),
				State:   "voter",
			}
			if server.Suffrage == raft.Nonvoter {
				n.stats[serverID].State = "nonvoter"
			}

			// If we have a join time, use it
			if jt, ok := n.joinTimes[serverID]; ok {
				n.stats[serverID].JoinTime = jt
			} else {
				// Otherwise set it to now
				n.stats[serverID].JoinTime = time.Now()
				n.joinTimes[serverID] = time.Now()
			}
		}
		n.statsMutex.Unlock()

		// Check if we have no contact information or never contacted
		if !ok || lastContactStr == "never" {
			n.handleFailedServer(serverID)
			continue
		}

		// Try to parse the last contact time
		lastContactDuration, err := time.ParseDuration(lastContactStr)
		if err != nil {
			log.Printf("warning: couldn't parse last contact time for %s: %v", serverID, err)
			continue
		}

		// Update the last contact time in our stats
		lastContactTime := time.Now().Add(-lastContactDuration)
		n.statsMutex.Lock()
		if s, exists := n.stats[serverID]; exists {
			s.LastContact = lastContactTime
		}
		n.statsMutex.Unlock()

		// If last contact was more than 30 seconds ago, consider it a failure
		if lastContactDuration > 30*time.Second {
			n.handleFailedServer(serverID)
		} else {
			// Reset failure counter if we've had recent contact
			n.failureDetector[serverID] = 0

			// Update failure count in stats
			n.statsMutex.Lock()
			if s, exists := n.stats[serverID]; exists {
				s.FailureCount = 0
			}
			n.statsMutex.Unlock()
		}
	}
}

// handleFailedServer increments failure counter and removes server if threshold is reached
func (n *RaftNode) handleFailedServer(serverID string) {
	// Check if this is a recently joined node (within 2 minutes)
	joinTime, exists := n.joinTimes[serverID]
	if exists && time.Since(joinTime) < 2*time.Minute {
		// Give new nodes a grace period
		log.Printf("server %s joined recently, giving it time to initialize", serverID)
		return
	}

	n.failureDetector[serverID]++

	// Update failure count in stats
	n.statsMutex.Lock()
	if s, exists := n.stats[serverID]; exists {
		s.FailureCount = n.failureDetector[serverID]
	}
	n.statsMutex.Unlock()

	failures := n.failureDetector[serverID]
	if failures == 1 || failures%5 == 0 {
		log.Printf("warning: server %s is unresponsive (%d/%d failed checks)",
			serverID, failures, n.failureThreshold)
	}

	// If we've reached the threshold, remove the server
	if failures >= n.failureThreshold {
		log.Printf("server %s has been unresponsive for too long, removing from configuration", serverID)
		if err := n.RemoveServer(serverID); err != nil {
			log.Printf("error removing server %s: %v", serverID, err)
		} else {
			log.Printf("successfully removed server %s from the cluster", serverID)
		}
	}
}

// monitorLeadership monitors leadership changes
func (n *RaftNode) monitorLeadership() {
	for {
		select {
		case <-n.monitorShutdown:
			return
		default:
			isLeader := n.raft.State() == raft.Leader

			// Track leadership acquisition
			n.statsMutex.Lock()
			// If we're now leader and we weren't before (leaderSince is zero time)
			if isLeader && n.leaderSince.IsZero() {
				n.leaderSince = time.Now()
				n.termTransitions++
				log.Printf("node %s became leader", n.nodeID)

				// Update our own state
				if s, exists := n.stats[n.nodeID]; exists {
					s.State = "leader"
				}
			} else if !isLeader && !n.leaderSince.IsZero() {
				// We were leader but aren't anymore
				leadershipDuration := time.Since(n.leaderSince)
				n.totalLeaderTime += leadershipDuration
				n.leaderSince = time.Time{} // Reset to zero time
				log.Printf("node %s lost leadership after %v", n.nodeID, leadershipDuration)

				// Update our own state
				if s, exists := n.stats[n.nodeID]; exists {
					s.State = n.getStateString()
				}
			}
			n.statsMutex.Unlock()

			// Sleep to avoid tight loop
			time.Sleep(1 * time.Second)
		}
	}
}

func (n *RaftNode) getStateString() string {
	switch n.raft.State() {
	case raft.Leader:
		return "leader"
	case raft.Candidate:
		return "candidate"
	case raft.Follower:
		return "follower"
	case raft.Shutdown:
		return "shutdown"
	default:
		return "unknown"
	}
}

func (n *RaftNode) collectStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Update our own stats
			n.statsMutex.Lock()
			if s, exists := n.stats[n.nodeID]; exists {
				s.State = n.getStateString()

				// If we're currently leader, update leadership time
				if n.raft.State() == raft.Leader && !n.leaderSince.IsZero() {
					currentLeadershipTime := time.Since(n.leaderSince)
					s.LeadershipTime = n.totalLeaderTime + currentLeadershipTime
				} else {
					s.LeadershipTime = n.totalLeaderTime
				}
			}
			n.statsMutex.Unlock()
		case <-n.monitorShutdown:
			return
		}
	}
}

func (n *RaftNode) GetClusterStats() map[string]ServerStats {
	n.statsMutex.RLock()
	defer n.statsMutex.RUnlock()

	result := make(map[string]ServerStats)
	for id, stats := range n.stats {
		result[id] = *stats
	}

	return result
}

func (n *RaftNode) GetNodeStats() map[string]interface{} {
	n.statsMutex.RLock()
	defer n.statsMutex.RUnlock()

	raftStats := n.raft.Stats()

	result := map[string]interface{}{
		"node_id":           n.nodeID,
		"state":             n.getStateString(),
		"term_transitions":  n.termTransitions,
		"applied_commands":  n.appliedCommands,
		"total_leader_time": n.totalLeaderTime.String(),
		"raft_stats":        raftStats,
	}

	if n.raft.State() == raft.Leader && !n.leaderSince.IsZero() {
		result["current_leadership_duration"] = time.Since(n.leaderSince).String()
	}

	return result
}
