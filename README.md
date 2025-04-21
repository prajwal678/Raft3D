# Raft3D - Distributed 3D Printer Management System

Raft3D is a distributed, fault-tolerant 3D printer management system built on the Raft consensus algorithm. It allows for the management of 3D printers, filaments, and print jobs with strong consistency guarantees across multiple nodes.

## Features

- distributed consensus: uses the Raft algorithm to maintain consistency across nodes
- fault tolerance: continues to operate even when nodes fail
- automatic leader election: elects a new leader when the current leader fails
- persistent storage: uses BoltDB for durable storage of the system state
- comprehensive logging: logs are stored in files with configurable verbosity
- health monitoring: automatically detects and removes unresponsive nodes
- restful API: simple HTTP API for managing printers, filaments, and print jobs

## Prerequisites

- Go 1.21 or higher
- tmux (for the automated testing scripts)
- jq (for JSON formatting in the testing scripts)

On Ubuntu/Debian:
```bash
sudo apt install tmux jq
```

## Running Raft3D

You can run Raft3D either manually from the terminal or using the provided scripts for automated setup and testing.

### Manual Cluster Setup (Terminal)

#### 1. Start the bootstrap node

The first node needs to bootstrap the cluster:

```bash
go run main.go -id node1 -http :8080 -raft 127.0.0.1:7000 -data ./data -bootstrap -console-level info -file-level debug -logs ./logs
```

This creates a new Raft cluster with a single node and starts the HTTP API server on port 8080.

#### 2. Add additional nodes

Once the bootstrap node is running, you can add additional nodes:

```bash
# Second node
go run main.go -id node2 -http :8081 -raft 127.0.0.1:7001 -data ./data -join 127.0.0.1:8080 -console-level info -file-level debug -logs ./logs

# Third node
go run main.go -id node3 -http :8082 -raft 127.0.0.1:7002 -data ./data -join 127.0.0.1:8080 -console-level info -file-level debug -logs ./logs
```

The `-join` parameter specifies the HTTP address of any existing node in the cluster.

#### 3. Interact with the cluster via API

Once the cluster is running, you can interact with it using HTTP requests:

```bash
# Check cluster status
curl http://localhost:8080/cluster/status | jq

# Add a printer
curl -X POST -H "Content-Type: application/json" -d '{"id":"printer1","company":"Prusa","model":"MK3S+"}' http://localhost:8080/api/v1/printers

# Add a filament
curl -X POST -H "Content-Type: application/json" -d '{"id":"filament1","type":"PLA","color":"Red","total_weight_in_grams":1000,"remaining_weight_in_grams":1000}' http://localhost:8080/api/v1/filaments

# Add a print job
curl -X POST -H "Content-Type: application/json" -d '{"id":"job1","printer_id":"printer1","filament_id":"filament1","filepath":"/models/benchy.gcode","print_weight_in_grams":15}' http://localhost:8080/api/v1/print_jobs

# Update a print job status
curl -X POST "http://localhost:8080/api/v1/print_jobs/job1/status?status=Running"
```

### Automated Cluster Setup (Scripts)

Raft3D includes scripts to automate the setup and testing process:

#### 1. Reset data (if needed)

```bash
./reset_data.sh
```

This script:
- Stops any running Raft nodes
- Removes existing data and log directories
- Ensures file system syncs to prevent data corruption
- Creates fresh empty directories

#### 2. Start a cluster in tmux

```bash
./run_raft_cluster.sh
```

This script:
- Runs reset_data.sh to clean up existing data
- Creates a tmux session with 4 panes for Raft nodes and 1 for commands
- Starts a bootstrap node (node1)
- Starts 3 follower nodes with automatic joining
- Provides a command pane with example test commands

The resulting tmux session will look like:

```
initial config. monitor nodes on the left and run stuff on the right
+-----------------+-------------------------------+
| Node 1          |                               |
| (Bootstrap)     |                               |
+-----------------+                               |
| Node 2          |                               |
| (Follower)      |           test cmds           |
+-----------------+         (your window)         |
| Node 3          |         (to run cmds)         |
| (Follower)      |    run ./test_scenarios.sh    |
+-----------------+                               |
| Node 4          |                               |
| (Follower)      |                               |
+-----------------+-------------------------------+
```

#### 3. Run automated test scenarios

```bash
./test_scenarios.sh
```

This script:
- Checks that the tmux session is running
- Verifies cluster health and leader election
- Offers an interactive menu of tests:
  1. Basic Workflow Test (add printer, filament, job, update status)
  2. Node Failure and Recovery Test
  3. Leader Failure and Election Test
  4. Run All Tests
- Executes the selected tests and reports results

#### tmux Controls

When in the tmux session:
- To detach (leave the session running): `Ctrl+B, then D`
- To reattach to a running session: `tmux attach-session -t raft3d`
- To kill the session: `tmux kill-session -t raft3d`
- To scroll in a pane: `Ctrl+B, then [` (use arrow keys to scroll, press `q` to exit scroll mode)

## RESTful API

The system provides a RESTful API for managing printers, filaments, and print jobs.

### Endpoints

#### Printers
- **GET /api/v1/printers** - List all printers
- **POST /api/v1/printers** - Add a new printer

#### Filaments
- **GET /api/v1/filaments** - List all filaments
- **POST /api/v1/filaments** - Add a new filament

#### Print Jobs
- **GET /api/v1/print_jobs** - List all print jobs
- **POST /api/v1/print_jobs** - Add a new print job
- **POST /api/v1/print_jobs/{id}/status?status={status}** - Update a print job status

#### Cluster Management
- **GET /cluster/status** - Get cluster status
- **POST /join** - Join a node to the cluster
- **DELETE /cluster/servers?id={nodeId}** - Remove a node from the cluster

## Architecture

Raft3D follows a layered architecture:
1. **API Layer**: Handles HTTP requests and routes them to the store
2. **Store Layer**: Implements the business logic and interacts with the Raft node
3. **Raft Layer**: Manages consensus and replication across nodes
4. **Storage Layer**: Uses BoltDB for durable storage

The system uses the Hashicorp Raft implementation for consensus, which ensures:
- Leader Election
- Log Replication
- Safety (never returning incorrect results)

## Advanced Usage

### Command-line Options

```bash
go run main.go [options]
  -id string            # Node ID (required)
  -http string          # HTTP service address (default ":8080")
  -raft string          # Raft transport address (default "127.0.0.1:7000")
  -data string          # Data directory (default "data")
  -logs string          # Log directory (default "logs")
  -join string          # Join address(es) (comma-separated)
  -bootstrap            # Bootstrap the cluster
  -clean                # Clean data directory before starting
  -console-level string # Console log level (debug, info, warn, error)
  -file-level string    # File log level (debug, info, warn, error)
```

### Cluster Maintenance

To manually remove a server from the cluster:
```bash
curl -X DELETE "http://localhost:8080/cluster/servers?id=node3"
```

To check the current leader:
```bash
curl http://localhost:8080/cluster/status | jq
```