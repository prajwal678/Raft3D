package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	handlers "raft3d/api"
	"raft3d/raftnode"
	"raft3d/store"
	"raft3d/utils"
)

// JoinRequest represents the request to join a Raft cluster
type JoinRequest struct {
	NodeID string `json:"node_id"`
	Addr   string `json:"addr"`
}

// Global logger instance
var logger *utils.Logger

// CleanDataDir removes the data directory for a fresh start
func CleanDataDir(dataDir string) error {
	// Check if directory exists
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to clean
		return nil
	}

	logger.Info("cleaning data directory: %s", dataDir)
	return os.RemoveAll(dataDir)
}

func main() {
	// Parse command line arguments
	var (
		nodeID          = flag.String("id", "", "Node ID (required)")
		httpAddr        = flag.String("http", ":8080", "HTTP service address")
		raftAddr        = flag.String("raft", "127.0.0.1:7000", "Raft transport address")
		dataDir         = flag.String("data", "data", "Data directory")
		logDir          = flag.String("logs", "logs", "Log directory")
		join            = flag.String("join", "", "Join address(es) (comma-separated list)")
		bootstrap       = flag.Bool("bootstrap", false, "Bootstrap the cluster")
		clean           = flag.Bool("clean", false, "Clean data directory before starting")
		consoleLogLevel = flag.String("console-level", "info", "Console log level (debug, info, warn, error)")
		fileLogLevel    = flag.String("file-level", "debug", "File log level (debug, info, warn, error)")
	)
	flag.Parse()

	// Validate arguments
	if *nodeID == "" {
		fmt.Fprintln(os.Stderr, "node id is required")
		os.Exit(1)
	}

	// Redirect standard log output to discard to suppress bolt warnings
	log.SetOutput(ioutil.Discard)

	// Set up logging
	// Parse log levels
	consoleLevel := parseLogLevel(*consoleLogLevel)
	fileLevel := parseLogLevel(*fileLogLevel)

	var err error
	logger, err = utils.NewLogger(*nodeID, *logDir, consoleLevel, fileLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to set up logger: %v\n", err)
		os.Exit(1)
	}

	logger.Info("starting raft3d node: id=%s, http=%s, raft=%s", *nodeID, *httpAddr, *raftAddr)

	// Clean data directory if requested
	if *clean {
		nodeDataDir := filepath.Join(*dataDir, *nodeID)
		if err := CleanDataDir(nodeDataDir); err != nil {
			logger.Error("failed to clean data directory: %v", err)
			os.Exit(1)
		}
	}

	// Create the data directory
	nodeDataDir := filepath.Join(*dataDir, *nodeID)
	if err := os.MkdirAll(nodeDataDir, 0755); err != nil {
		logger.Error("failed to create data directory: %v", err)
		os.Exit(1)
	}

	// Parse join addresses
	var joinAddrs []string
	if *join != "" {
		joinAddrs = strings.Split(*join, ",")
	}

	// Create the Raft node
	logger.Info("creating raft node with data directory: %s", nodeDataDir)
	raftNode, err := raftnode.NewRaftNode(*nodeID, *raftAddr, nodeDataDir, *bootstrap)
	if err != nil {
		logger.Error("failed to create raft node: %v", err)
		os.Exit(1)
	}

	// Create the store
	s := store.NewStore(raftNode)

	// Join the cluster if needed
	if len(joinAddrs) > 0 && !*bootstrap {
		// Join existing cluster by contacting the join address
		logger.Info("joining cluster from %s", joinAddrs[0])

		joinAddr := joinAddrs[0]
		// If the join address doesn't have a port, add the default HTTP port
		if !strings.Contains(joinAddr, ":") {
			joinAddr = joinAddr + ":8080"
		}

		// Ensure the address has http:// prefix
		if !strings.HasPrefix(joinAddr, "http://") {
			joinAddr = "http://" + joinAddr
		}

		// Prepare the join request
		joinReq := JoinRequest{
			NodeID: *nodeID,
			Addr:   *raftAddr,
		}

		reqBody, err := json.Marshal(joinReq)
		if err != nil {
			logger.Error("failed to marshal join request: %v", err)
			os.Exit(1)
		}

		// Try to join with retries
		maxRetries := 10
		retryInterval := 2 * time.Second
		joined := false

		for i := 0; i < maxRetries; i++ {
			logger.Info("attempting to join cluster (try %d/%d)", i+1, maxRetries)
			resp, err := http.Post(joinAddr+"/join", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				logger.Warn("failed to join cluster, retrying (%d/%d): %v", i+1, maxRetries, err)
				time.Sleep(retryInterval)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				joined = true
				logger.Info("successfully joined the cluster")
				break
			}

			// Handle redirection to new leader
			if resp.StatusCode == http.StatusTemporaryRedirect {
				var redirect map[string]string
				if err := json.Unmarshal(body, &redirect); err == nil {
					if newLeader := redirect["leader"]; newLeader != "" {
						// Construct new join address using the new leader's Raft address
						// Extract the host from the Raft address
						leaderParts := strings.Split(newLeader, ":")
						if len(leaderParts) > 0 {
							host := strings.Split(leaderParts[0], "@")[0]

							// If host is empty, use localhost
							if host == "" {
								host = "127.0.0.1"
							}

							// Use the HTTP port corresponding to the node
							// This assumes HTTP ports follow the same pattern as Raft ports
							// E.g., if Raft is 7000, 7001, 7002, then HTTP is 8080, 8081, 8082
							raftPort, err := strconv.Atoi(leaderParts[1])
							if err == nil {
								// Translate 7000->8080, 7001->8081, etc.
								portDiff := 1080
								httpPort := raftPort + portDiff
								newJoinAddr := fmt.Sprintf("http://%s:%d", host, httpPort)

								logger.Info("redirecting join request to new leader at %s", newJoinAddr)
								joinAddr = newJoinAddr

								// Don't count this as a retry
								i--
								time.Sleep(retryInterval)
								continue
							}
						}
					}
				}
			}

			logger.Warn("failed to join cluster, retrying (%d/%d): status=%d, body=%s",
				i+1, maxRetries, resp.StatusCode, string(body))
			time.Sleep(retryInterval)
		}

		if !joined {
			logger.Error("failed to join the cluster after %d attempts", maxRetries)
			os.Exit(1)
		}
	}

	// Create the HTTP handler
	h := handlers.NewHandler(s)

	// Add middleware to log all requests
	loggingMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logger.Debug("request: %s %s", r.Method, r.URL.Path)
			next.ServeHTTP(w, r)
			logger.Debug("request completed: %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		})
	}

	// Create router and wrap with middleware
	router := http.NewServeMux()

	// Set up API routes
	router.HandleFunc("/api/v1/printers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.CreatePrinter(w, r)
		case http.MethodGet:
			h.GetPrinters(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	router.HandleFunc("/api/v1/filaments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.CreateFilament(w, r)
		case http.MethodGet:
			h.GetFilaments(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	router.HandleFunc("/api/v1/print_jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.CreatePrintJob(w, r)
		case http.MethodGet:
			h.GetPrintJobs(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	router.HandleFunc("/api/v1/print_jobs/", func(w http.ResponseWriter, r *http.Request) {
		// Expecting path: /api/v1/print_jobs/{id}/status
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Extract job ID
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 6 {
			http.Error(w, "invalid URL", http.StatusBadRequest)
			return
		}
		jobID := parts[4]
		status := r.URL.Query().Get("status")
		if status == "" {
			http.Error(w, "missing status parameter", http.StatusBadRequest)
			return
		}
		h.UpdatePrintJobStatus(w, r, jobID, status)
	})

	// Add cluster management endpoints
	// Join endpoint for cluster management
	router.HandleFunc("/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req JoinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		// Validate the request
		if req.NodeID == "" || req.Addr == "" {
			http.Error(w, "nodeID and addr are required", http.StatusBadRequest)
			return
		}

		logger.Info("received join request: NodeID=%s, Addr=%s", req.NodeID, req.Addr)

		// Check if this node is the leader
		if !raftNode.IsLeader() {
			leader := raftNode.GetLeader()
			logger.Info("not the leader, redirecting to %s", leader)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTemporaryRedirect)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "not the leader",
				"leader": leader,
			})
			return
		}

		logger.Info("adding new server to raft cluster: %s at %s", req.NodeID, req.Addr)
		if err := raftNode.AddServer(req.NodeID, req.Addr); err != nil {
			logger.Error("failed to add node to cluster: %v", err)
			http.Error(w, fmt.Sprintf("failed to add node to cluster: %v", err), http.StatusInternalServerError)
			return
		}

		logger.Info("node %s successfully joined the cluster", req.NodeID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "success",
			"leader": raftNode.GetLeader(),
		})
	})

	// Remove server endpoint
	router.HandleFunc("/cluster/servers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			nodeID := r.URL.Query().Get("id")
			if nodeID == "" {
				http.Error(w, "node id required", http.StatusBadRequest)
				return
			}

			if !raftNode.IsLeader() {
				leader := raftNode.GetLeader()
				logger.Info("not the leader, redirecting to %s", leader)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTemporaryRedirect)
				json.NewEncoder(w).Encode(map[string]string{
					"error":  "not the leader",
					"leader": leader,
				})
				return
			}

			logger.Info("removing server %s from the cluster", nodeID)
			if err := raftNode.RemoveServer(nodeID); err != nil {
				logger.Error("failed to remove server: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "removed",
				"node_id": nodeID,
			})
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// Add cluster info endpoint
	router.HandleFunc("/cluster/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		isLeader := raftNode.IsLeader()
		leader := raftNode.GetLeader()

		response := map[string]interface{}{
			"node_id":   *nodeID,
			"is_leader": isLeader,
			"leader":    leader,
			"raft_addr": *raftAddr,
			"http_addr": *httpAddr,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	// Set up graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down...")
		raftNode.Shutdown()
		os.Exit(0)
	}()

	// Start the HTTP server
	logger.Info("raft3d node %s running http server on %s and raft on %s",
		*nodeID, *httpAddr, *raftAddr)

	// Create server with timeout settings
	server := &http.Server{
		Addr:         *httpAddr,
		Handler:      loggingMiddleware(router),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	logger.Error("server error: %v", server.ListenAndServe())
}

// parseLogLevel converts a string log level to the corresponding LogLevel
func parseLogLevel(level string) utils.LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return utils.LevelDebug
	case "info":
		return utils.LevelInfo
	case "warn", "warning":
		return utils.LevelWarning
	case "error":
		return utils.LevelError
	default:
		return utils.LevelInfo // Default to info level
	}
}
