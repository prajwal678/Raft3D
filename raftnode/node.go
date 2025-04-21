package raftnode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"raft3d/fsm"
	"raft3d/models"
	"raft3d/utils"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// FailureDetector is a map that tracks server failure counts
type FailureDetector map[string]int

// NewFailureDetector creates a new failure detector with given capacity
func NewFailureDetector(capacity int) *FailureDetector {
	fd := make(FailureDetector, capacity)
	return &fd
}

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
	nodeID          string
	raftAddr        string
	dataDir         string
	bootstrap       bool
	raft            *raft.Raft
	fsm             *fsm.PrinterFSM
	transport       raft.Transport
	logStore        raft.LogStore
	stableStore     raft.StableStore
	snapshotStore   raft.SnapshotStore
	joinedServers   map[string]string // map of server id to raft address
	mu              sync.Mutex
	failureDetector *FailureDetector
	previousLeader  string
	leaderLostTime  time.Time
	leaderless      bool // track if there's no leader
	logger          *utils.Logger

	failureThreshold int // Number of failures before removing a server
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

func NewRaftNode(nodeID, raftAddr, dataDir string, bootstrap bool, joinAddr string) (*RaftNode, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("error creating data directory: %w", err)
	}

	// Create a logger for this node
	logger, err := utils.NewLogger(nodeID, "logs", utils.LevelInfo, utils.LevelDebug)
	if err != nil {
		return nil, fmt.Errorf("error creating logger: %w", err)
	}

	// Create the FSM
	rn := &RaftNode{
		nodeID:        nodeID,
		raftAddr:      raftAddr,
		dataDir:       dataDir,
		bootstrap:     bootstrap,
		joinedServers: make(map[string]string),
		logger:        logger,
	}

	fsm := fsm.NewPrinterFSM()
	rn.fsm = fsm

	// Create the log store and stable store
	boltDB, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft.db"))
	if err != nil {
		return nil, fmt.Errorf("error creating BoltDB log store: %w", err)
	}
	rn.logStore = boltDB
	rn.stableStore = boltDB

	// Create the snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(dataDir, 3, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("error creating snapshot store: %w", err)
	}
	rn.snapshotStore = snapshotStore

	// Setup the transport
	addr, err := net.ResolveTCPAddr("tcp", raftAddr)
	if err != nil {
		return nil, fmt.Errorf("error resolving TCP address: %w", err)
	}
	transport, err := raft.NewTCPTransport(raftAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("error creating TCP transport: %w", err)
	}
	rn.transport = transport

	// Create a failure detector
	rn.failureDetector = NewFailureDetector(5)

	// Setup Raft configuration
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(nodeID)
	// Tune timeouts for better election stability and reduced loss of leadership
	config.HeartbeatTimeout = 500 * time.Millisecond   // Faster heartbeats to detect failure quicker
	config.ElectionTimeout = 1000 * time.Millisecond   // Faster elections
	config.CommitTimeout = 100 * time.Millisecond      // Faster commits
	config.SnapshotInterval = 24 * time.Hour           // Longer snapshot interval as we don't need frequent snapshots
	config.SnapshotThreshold = 10240                   // Increase snapshot threshold
	config.LeaderLeaseTimeout = 500 * time.Millisecond // Must be <= HeartbeatTimeout

	// Configure logging - suppress most Raft logs
	config.LogOutput = io.Discard

	// Create the Raft instance
	r, err := raft.NewRaft(config, rn.fsm, rn.logStore, rn.stableStore, rn.snapshotStore, rn.transport)
	if err != nil {
		return nil, fmt.Errorf("error creating Raft: %w", err)
	}
	rn.raft = r

	// Bootstrap the cluster if needed
	if bootstrap {
		rn.logger.Info("bootstrapping cluster with node: %s", nodeID)
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:       raft.ServerID(nodeID),
					Address:  raft.ServerAddress(raftAddr),
					Suffrage: raft.Voter,
				},
			},
		}
		rn.raft.BootstrapCluster(configuration)
	} else if joinAddr != "" {
		// Join an existing cluster
		rn.logger.Info("joining existing cluster at %s", joinAddr)
		err = rn.joinCluster(joinAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to join cluster: %w", err)
		}
	}

	// Start monitoring leadership
	go rn.monitorLeadership()

	rn.failureThreshold = 20
	rn.monitorShutdown = make(chan struct{})
	rn.lastContactTimes = make(map[string]time.Time)
	rn.joinTimes = make(map[string]time.Time)
	rn.stats = make(map[string]*ServerStats)
	rn.termTransitions = 0
	rn.appliedCommands = 0

	go rn.collectStats()

	rn.statsMutex.Lock()
	rn.stats[nodeID] = &ServerStats{
		JoinTime: time.Now(),
		Address:  raftAddr,
		State:    rn.getStateString(),
	}
	rn.statsMutex.Unlock()

	return rn, nil
}

// joinCluster sends a request to join an existing Raft cluster
func (rn *RaftNode) joinCluster(joinAddr string) error {
	// Ensure the address has http:// prefix
	if !strings.HasPrefix(joinAddr, "http://") {
		joinAddr = "http://" + joinAddr
	}

	joinURL := fmt.Sprintf("%s/join", joinAddr)

	// Prepare the join request
	reqBody, err := json.Marshal(map[string]string{
		"node_id": rn.nodeID,
		"addr":    rn.raftAddr,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal join request: %w", err)
	}

	// Send the join request
	resp, err := http.Post(joinURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to join cluster: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to join cluster: %s", string(body))
	}

	return nil
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
	leaderAddr := string(n.raft.Leader())

	// If we have a valid leader, return it
	if leaderAddr != "" && leaderAddr != "@" {
		return leaderAddr
	}

	// If no leader is known, check all servers in the configuration
	// This helps during leader transitions
	configFuture := n.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		log.Printf("error getting raft configuration: %v", err)
		return ""
	}

	for _, server := range configFuture.Configuration().Servers {
		if server.ID != raft.ServerID(n.nodeID) { // Don't return ourselves
			serverAddr := string(server.Address)
			if serverAddr != "" {
				// Return the first other server we find
				// The redirect process will eventually find the leader
				return serverAddr
			}
		}
	}

	return ""
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

	// Close transport if possible
	if tcpTransport, ok := n.transport.(*raft.NetworkTransport); ok {
		tcpTransport.Close()
	}

	if logStore, ok := n.logStore.(io.Closer); ok {
		if err := logStore.Close(); err != nil {
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

	future := n.raft.Apply(cmdData, 10*time.Second)
	return future.Error()
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

	future := n.raft.Apply(cmdData, 10*time.Second)
	return future.Error()
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

	future := n.raft.Apply(cmdData, 10*time.Second)
	return future.Error()
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

	future := n.raft.Apply(cmdData, 10*time.Second)
	return future.Error()
}

func (n *RaftNode) RemoveServer(nodeID string) error {
	if n.raft.State() != raft.Leader {
		return errors.New("not the leader")
	}

	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, 0)
	if err := future.Error(); err != nil {
		return err
	}

	// Remove the server from our tracking
	delete(n.joinTimes, nodeID)

	// Safely dereference the failureDetector before deleting an entry
	if n.failureDetector != nil {
		fd := *n.failureDetector
		delete(fd, nodeID)
	}

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
			// Safely dereference the failureDetector
			if n.failureDetector != nil {
				fd := *n.failureDetector
				fd[serverID] = 0
			}

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
		n.logger.Info("server %s joined recently, giving it time to initialize", serverID)
		return
	}

	// Safely access and update the failureDetector map
	var failures int
	if n.failureDetector != nil {
		fd := *n.failureDetector
		fd[serverID]++
		failures = fd[serverID]

		// Update failure count in stats
		n.statsMutex.Lock()
		if s, exists := n.stats[serverID]; exists {
			s.FailureCount = failures
		}
		n.statsMutex.Unlock()
	}

	if failures == 1 || failures%5 == 0 {
		n.logger.Warn("server %s is unresponsive (%d/%d failed checks)",
			serverID, failures, n.failureThreshold)
	}

	// If we've reached the threshold, remove the server
	if failures >= n.failureThreshold {
		n.logger.Warn("server %s has been unresponsive for too long, removing from configuration", serverID)
		if err := n.RemoveServer(serverID); err != nil {
			n.logger.Error("error removing server %s: %v", serverID, err)
		} else {
			n.logger.Info("successfully removed server %s from the cluster", serverID)
		}
	}
}

// monitorLeadership monitors leadership changes
func (n *RaftNode) monitorLeadership() {
	observedLeader := ""
	leaderlessTime := time.Time{}
	sleepInterval := 500 * time.Millisecond // More responsive checking

	for {
		leaderAddr := n.GetLeader()

		// If leader has changed, log it
		if leaderAddr != observedLeader {
			n.logger.Info("leader changed from '%s' to '%s'", observedLeader, leaderAddr)
			n.previousLeader = observedLeader
			observedLeader = leaderAddr

			// Reset leaderless tracking if we have a leader
			if leaderAddr != "" {
				leaderlessTime = time.Time{}
				n.leaderless = false
			} else {
				// Start tracking leaderless time
				if leaderlessTime.IsZero() {
					leaderlessTime = time.Now()
					n.leaderLostTime = leaderlessTime
					n.leaderless = true
				}
			}
		}

		// If we've been leaderless for too long, try to trigger an election
		if leaderAddr == "" && !leaderlessTime.IsZero() {
			leaderlessDuration := time.Since(leaderlessTime)
			if leaderlessDuration > 3*time.Second {
				// Check if this node is eligible to be a candidate
				state := n.raft.State()
				if state != raft.Leader && state != raft.Candidate {
					n.logger.Info("no leader detected for too long, checking server health")

					// Get configuration to see server list
					future := n.raft.GetConfiguration()
					if err := future.Error(); err == nil {
						// Request a leader check which might trigger an election
						future := n.raft.VerifyLeader()
						if err := future.Error(); err != nil {
							n.logger.Warn("leader verification failed: %v", err)
						}
					}
				}

				// Only reset the timer if we've taken action, otherwise keep trying
				leaderlessTime = time.Now()
			}
		}

		time.Sleep(sleepInterval)
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
			// Update state for this node
			n.statsMutex.Lock()
			if stat, ok := n.stats[n.nodeID]; ok {
				stat.State = n.getStateString()

				// Update leadership time if we're the leader
				if stat.State == "leader" {
					if n.leaderSince.IsZero() {
						n.leaderSince = time.Now()
					}
					stat.LeadershipTime = time.Since(n.leaderSince)
				} else {
					// Reset leader since if we're not leader
					if !n.leaderSince.IsZero() {
						n.totalLeaderTime += time.Since(n.leaderSince)
						n.leaderSince = time.Time{}
					}
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
	state := n.raft.State().String()
	isLeader := (state == "Leader")

	return map[string]interface{}{
		"node_id":           n.nodeID,
		"state":             state,
		"is_leader":         isLeader,
		"leader":            n.GetLeader(),
		"term_transitions":  n.termTransitions,
		"applied_commands":  n.appliedCommands,
		"total_leader_time": n.totalLeaderTime.String(),
	}
}
