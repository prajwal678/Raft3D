# Raft3D - Practical Implementation Guide

This document provides practical implementation details, usage examples, and in-depth explanation of how the different components of Raft3D work together. Read this alongside `ARCHITECTURE.md` for a complete understanding of the system.

## Table of Contents

1. [System Design Goals](#1-system-design-goals)
2. [Implementation Deep Dive](#2-implementation-deep-dive)
3. [Practical Configuration Guide](#3-practical-configuration-guide)
4. [Troubleshooting Guide](#4-troubleshooting-guide)
5. [Implementation Patterns](#5-implementation-patterns)
6. [Operational Scenarios](#6-operational-scenarios)

## 1. System Design Goals

### 1.1 Why Raft Consensus?

Raft3D uses the Raft consensus algorithm for several key reasons:

- **Understandability**: Raft is designed to be more understandable than alternatives like Paxos
- **Strong Leader**: Raft's strong leader approach simplifies the replication process
- **Leader Election**: Provides a clear process for handling node failures
- **Membership Changes**: Allows for dynamic cluster reconfiguration

### 1.2 Design Principles

1. **Consistency Over Availability**: When forced to choose, the system favors consistency
2. **Automatic Recovery**: System self-heals without manual intervention
3. **Graceful Degradation**: Continues operating with reduced node count
4. **Deterministic Behavior**: System behavior is predictable and reproducible

### 1.3 Performance vs. Consistency Tradeoffs

- **Write Operations**: All write operations go through the leader and require consensus
- **Read Operations**: Currently all reads also go through leader for consistency
- **Timeouts**: Balanced to provide reasonable recovery time without unnecessary elections

## 2. Implementation Deep Dive

### 2.1 Raft Consensus Details

#### 2.1.1 Log Structure

Each node maintains a log of operations with these components:

- **Index**: Position in the log (monotonically increasing)
- **Term**: Election term when entry was created
- **Command**: Serialized command to apply to FSM

Log entries are considered **committed** once they've been replicated to a majority of nodes. Only committed entries are applied to the FSM.

#### 2.1.2 Term Numbers

Term numbers are a logical clock that:
- Increase monotonically
- Start at 1
- Increment during election attempts
- Help identify outdated information

#### 2.1.3 Election Process Detailed Steps

1. **Follower to Candidate**:
   - Increment current term
   - Vote for self
   - Reset election timer
   - Send RequestVote RPCs to all other nodes

2. **Vote Granting**:
   - Nodes grant vote if:
     - Candidate's term ≥ receiver's term
     - Receiver hasn't voted for another candidate in this term
     - Candidate's log is at least as up-to-date as receiver's log

3. **Candidate to Leader**:
   - If majority votes received, become leader
   - Begin sending heartbeats (empty AppendEntries RPCs)
   - Start accepting client requests

4. **Split Vote Handling**:
   - If election timeout occurs without winner, new election begins
   - Random election timeouts prevent continuous split votes

### 2.2 State Machine Implementation

#### 2.2.1 Command Processing Flow

```
Client Request → API → Store → RaftNode → Raft Consensus → Log Replication → Majority Commit → FSM Apply
```

#### 2.2.2 PrinterFSM Internals

The state machine maintains three core data structures:
- `printers`: Map of printerID to Printer object
- `filaments`: Map of filamentID to Filament object
- `printJobs`: Map of jobID to PrintJob object

All state modifications happen through the `Apply` method:

```go
func (f *PrinterFSM) Apply(log *raft.Log) interface{} {
    var cmd Command
    json.Unmarshal(log.Data, &cmd)
    
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
```

#### 2.2.3 State Changes and Side Effects

Certain state changes trigger side effects:
- **Job Status → Done**: Reduces filament's remaining weight
- **Server Removal**: Cleans up monitoring data for that server
- **Leader Change**: Updates leadership tracking metrics

### 2.3 Failure Detection Mechanism

#### 2.3.1 FailureDetector Implementation

The FailureDetector is a simple but effective mechanism:
```go
type FailureDetector map[string]int

func NewFailureDetector(capacity int) *FailureDetector {
    fd := make(FailureDetector, capacity)
    return &fd
}
```

It tracks failure counts per server and:
- Increments when contact fails
- Resets to zero when contact succeeds
- Triggers removal when count exceeds threshold

#### 2.3.2 Server Health Monitoring

The leader regularly checks all server health:
```go
func (n *RaftNode) checkServers() {
    // For each server:
    // 1. Get last contact time from Raft stats
    // 2. If no contact for >30s, call handleFailedServer()
    // 3. Otherwise reset failure counter
}
```

Health checking uses multiple signals:
- Raft's internal last contact tracking
- Explicit heartbeat responses
- Leader AppendEntries responses

### 2.4 Storage Layer Internals

#### 2.4.1 BoltDB Usage Details

BoltDB provides:
- Key-value storage with nested buckets
- ACID transactions
- Single-writer, multiple-reader access

Raft uses three distinct storage components:
1. **Log Store**: Stores log entries by index
2. **Stable Store**: Stores metadata (currentTerm, votedFor)
3. **Snapshot Store**: Stores periodic state snapshots

#### 2.4.2 Data Persistence Layout

```
data/
├── node1/
│   ├── raft.db         # BoltDB file with logs and stable store
│   └── snapshots/      # Directory of snapshot files
├── node2/
│   ├── raft.db
│   └── snapshots/
└── node3/
    ├── raft.db
    └── snapshots/
```

#### 2.4.3 Recovery Process

System recovery process after restart:
1. Read stable store for term and vote information
2. Load most recent snapshot if available
3. Replay any log entries after snapshot
4. Start accepting new requests

## 3. Practical Configuration Guide

### 3.1 Tuning Parameters

#### 3.1.1 Timeout Settings

```go
config.HeartbeatTimeout = 500 * time.Millisecond   // Faster heartbeats to detect failure quicker
config.ElectionTimeout = 1000 * time.Millisecond   // Faster elections
config.CommitTimeout = 100 * time.Millisecond      // Faster commits
config.LeaderLeaseTimeout = 500 * time.Millisecond // Must be <= HeartbeatTimeout
```

**Considerations**:
- **Shorter Timeouts**: Faster recovery, but more network traffic
- **Longer Timeouts**: Less resource usage, but slower recovery
- **Timeout Relationships**:
  - ElectionTimeout > HeartbeatTimeout (typically 2x)
  - LeaderLeaseTimeout <= HeartbeatTimeout

#### 3.1.2 Health Check Settings

```go
// Check frequency
ticker := time.NewTicker(10 * time.Second)

// Failure threshold
// In RaftNode initialization
rn.failureThreshold = 20

// Failure detection threshold (in checkServers)
if lastContactDuration > 30*time.Second {
    n.handleFailedServer(serverID)
}
```

**Considerations**:
- **Check Frequency**: How often to check server health
- **Failure Threshold**: How many consecutive failures before removal
- **Detection Threshold**: How long without contact to consider a failure

#### 3.1.3 Snapshot Settings

```go
config.SnapshotInterval = 24 * time.Hour           // Longer snapshot interval
config.SnapshotThreshold = 10240                   // Increase snapshot threshold
```

**Considerations**:
- **Interval**: Time-based snapshots
- **Threshold**: Log-size-based snapshots
- **Tradeoffs**: More frequent snapshots reduce recovery time but use more resources

### 3.2 Scaling Considerations

#### 3.2.1 Cluster Size Recommendations

- **Minimum**: 3 nodes (tolerates 1 failure)
- **Recommended**: 5 nodes (tolerates 2 failures)
- **Maximum Practical**: 7 nodes (diminishing returns after this)

Fault tolerance formula: `N/2+1` nodes must be available

#### 3.2.2 Resource Requirements

Per node:
- **CPU**: 1-2 cores (more for higher throughput)
- **Memory**: 512MB minimum, 1-2GB recommended
- **Disk**: 1GB minimum, depends on data volume and snapshot retention
- **Network**: Low-latency connections between nodes

#### 3.2.3 Network Configuration

- **Intra-Cluster**: Nodes should have reliable, low-latency connections to each other
- **Client Access**: API endpoints can be load-balanced, with leader redirection

### 3.3 Production Deployment Tips

#### 3.3.1 Docker Deployment

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o raft3d

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/raft3d /app/
VOLUME ["/app/data", "/app/logs"]
EXPOSE 8080 7000
CMD ["/app/raft3d", "-id", "${NODE_ID}", "-http", ":8080", "-raft", "0.0.0.0:7000", "-data", "/app/data", "-logs", "/app/logs"]
```

#### 3.3.2 Kubernetes Deployment Sketch

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: raft3d
spec:
  serviceName: "raft3d"
  replicas: 3
  selector:
    matchLabels:
      app: raft3d
  template:
    metadata:
      labels:
        app: raft3d
    spec:
      containers:
      - name: raft3d
        image: raft3d:latest
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 7000
          name: raft
        env:
        - name: NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        volumeMounts:
        - name: data
          mountPath: /app/data
        - name: logs
          mountPath: /app/logs
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 1Gi
  - metadata:
      name: logs
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 1Gi
```

## 4. Troubleshooting Guide

### 4.1 Common Issues and Solutions

#### 4.1.1 No Leader Elected

**Symptoms**:
- All nodes remain in follower state
- Operations fail with "not the leader" errors
- No leader shown in `/cluster/status`

**Possible Causes and Solutions**:
1. **Network Issues**: Ensure nodes can communicate on Raft ports
2. **Clock Skew**: Synchronize clocks between nodes
3. **Log Divergence**: Check logs for "term mismatch" errors, may need to reset state

#### 4.1.2 Split Brain

**Symptoms**:
- Different nodes report different leaders
- Inconsistent results from different nodes

**Possible Causes and Solutions**:
1. **Network Partition**: Fix network connectivity
2. **Quorum Issues**: Ensure proper cluster size (odd number of nodes)

#### 4.1.3 Excessive Leader Elections

**Symptoms**:
- Frequent leadership changes
- Performance degradation
- Log messages showing repeated elections

**Possible Causes and Solutions**:
1. **Network Instability**: Improve network reliability
2. **Timeout Too Short**: Increase heartbeat and election timeouts
3. **Overloaded Leader**: Reduce load or increase resources

### 4.2 Diagnosing Issues

#### 4.2.1 Key Log Patterns

Look for these log patterns:

1. **Leadership Changes**:
```
[INFO] leader changed from '127.0.0.1:7000' to '127.0.0.1:7001'
```

2. **Election Issues**:
```
[WARN] leader verification failed: not leader
```

3. **Server Failures**:
```
[WARN] server node2 is unresponsive (5/20 failed checks)
```

#### 4.2.2 Diagnostic Commands

1. **Check Cluster Status**:
```bash
curl http://localhost:8080/cluster/status | jq
```

2. **Verify Data Consistency**:
```bash
# Run on each node and compare outputs
curl http://localhost:8080/api/v1/printers | jq
curl http://localhost:8081/api/v1/printers | jq
curl http://localhost:8082/api/v1/printers | jq
```

3. **Force Remove Problematic Node**:
```bash
curl -X DELETE "http://localhost:8080/cluster/servers?id=node3"
```

### 4.3 Recovery Procedures

#### 4.3.1 Full Cluster Reset

If the cluster is in an unrecoverable state:

1. Stop all nodes
2. Run `./reset_data.sh`
3. Start bootstrap node
4. Join remaining nodes

#### 4.3.2 Single Node Recovery

To recover a single failed node:

1. Stop the problematic node
2. Delete its data directory (`rm -rf data/node_id/*`)
3. Restart the node with `-join` pointing to an existing node

#### 4.3.3 Leader Demotion

If a leader is problematic but functioning:

1. Force a leader step-down (not directly supported, but can be achieved by)
2. Temporarily isolate current leader network
3. Wait for new leader to be elected
4. Restore network connectivity

## 5. Implementation Patterns

### 5.1 Command Handling Pattern

The system uses a consistent pattern for handling commands:

1. **Client Request**: Receives via HTTP API
2. **Validation**: Performed at API and Store layers
3. **Command Creation**: Structured with type and data
4. **Raft Application**: Applied to Raft log
5. **Replication**: Sent to majority of nodes
6. **Commitment**: Once majority confirms
7. **FSM Application**: Command executed on state machine
8. **Response**: Result returned to client

```go
// Example command flow for adding a printer
func (n *RaftNode) AddPrinter(printer models.Printer) error {
    // 1. Create command
    data, _ := json.Marshal(printer)
    cmd := fsm.Command{
        Type: fsm.CommandAddPrinter,
        Data: json.RawMessage(data),
    }
    
    // 2. Marshal command
    cmdData, _ := json.Marshal(cmd)
    
    // 3. Apply to Raft log (triggers replication)
    future := n.raft.Apply(cmdData, 10*time.Second)
    
    // 4. Wait for consensus and return result
    return future.Error()
}
```

### 5.2 State Machine Update Pattern

The state machine uses a consistent pattern:

```go
func (f *PrinterFSM) applyAddPrinter(data []byte) interface{} {
    // 1. Unmarshal data
    var printer models.Printer
    if err := json.Unmarshal(data, &printer); err != nil {
        return fmt.Errorf("failed to unmarshal printer: %s", err)
    }

    // 2. Lock for thread safety
    f.mu.Lock()
    defer f.mu.Unlock()

    // 3. Apply to state
    f.printers[printer.ID] = printer
    
    // 4. Return result (nil or error)
    return nil
}
```

### 5.3 Leader-Follower Interaction Pattern

All operations follow a leader-forward pattern:

1. Client can contact any node
2. If node is not leader, redirects to leader
3. Leader handles operation
4. Leader replicates to followers
5. Once majority confirm, operation completes

```go
// Example redirection in API
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
```

## 6. Operational Scenarios

### 6.1 Bootstrapping a New Cluster

**Steps**:
1. Start first node with `-bootstrap`:
```bash
./raft3d -id node1 -http :8080 -raft 127.0.0.1:7000 -data ./data -bootstrap
```

2. This node:
   - Creates a single-node Raft configuration
   - Becomes leader automatically
   - Initializes empty state

3. Start accepting operations immediately

### 6.2 Adding New Nodes

**Steps**:
1. Start new node with `-join`:
```bash
./raft3d -id node2 -http :8081 -raft 127.0.0.1:7001 -data ./data -join 127.0.0.1:8080
```

2. New node:
   - Contacts existing node via HTTP
   - Existing leader adds it to configuration
   - Begins log replication
   - Becomes a voting member

3. Process repeats for additional nodes

### 6.3 Handling Leader Failure

**Automatic Process**:
1. Leader fails or becomes unreachable
2. Followers detect missing heartbeats
3. After election timeout, a follower becomes candidate
4. Candidate requests votes
5. If majority vote, becomes new leader
6. New leader begins accepting operations
7. Clients redirect to new leader

**Recovery Timeline**:
- Detection: 500-1000ms (1-2 missed heartbeats)
- Election: ~1000ms
- Total: 1.5-2 seconds typically

### 6.4 Data Migration Scenarios

#### 6.4.1 Full Cluster Migration

To migrate the entire cluster:

1. Set up new nodes with new storage
2. Add new nodes to existing cluster
3. Wait for synchronization
4. Remove old nodes one by one

#### 6.4.2 Snapshot-Based Migration

To migrate using snapshots:

1. Take a snapshot from existing node
2. Copy snapshot to new environment
3. Start new nodes using snapshot
4. Begin operations with new cluster

### 6.5 Monitoring and Maintenance

#### 6.5.1 Key Metrics to Track

1. **Leadership Metrics**:
   - Current leader
   - Leadership transitions
   - Time without leader

2. **Node Health**:
   - Last contact times
   - Failure counts
   - Available nodes

3. **Performance Metrics**:
   - Command apply latency
   - Log replication latency
   - Log committed vs. applied index

#### 6.5.2 Regular Maintenance Tasks

1. **Log Management**:
   - Monitor log growth
   - Ensure snapshots are working
   - Rotate/clean up old logs

2. **Cluster Health Checks**:
   - Verify all nodes responsive
   - Check leader is stable
   - Ensure no excessive elections

3. **Backup Procedures**:
   - Regular BoltDB backups
   - Snapshot backups
   - Test recovery procedures

By understanding these implementation details and operational procedures, you'll be well-equipped to deploy, operate, and troubleshoot a Raft3D cluster in production environments. 