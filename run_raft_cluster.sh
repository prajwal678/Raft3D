#!/bin/bash

SESSION_NAME="raft3d"
DATA_DIR="./data"
LOG_DIR="./logs"
DELAY_BETWEEN_NODES=5

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${GREEN}Setting up Raft3D cluster test environment...${NC}"

echo -e "${YELLOW}Cleaning data and logs directories...${NC}"
./reset_data.sh
echo "Done."

if ! command -v tmux &> /dev/null; then
    echo -e "${YELLOW}tmux is not installed. Please install it first.${NC}"
    echo "For Ubuntu/Debian: sudo apt install tmux"
    echo "For CentOS/RHEL: sudo yum install tmux"
    echo "For macOS: brew install tmux"
    exit 1
fi
# not adding for arch cuz they should know, if not shouldn't be using arch

tmux kill-session -t "$SESSION_NAME" 2>/dev/null

echo -e "${YELLOW}Creating tmux session...${NC}"
tmux new-session -d -s "$SESSION_NAME" -n "raft-nodes"

tmux split-window -h -t "$SESSION_NAME:0.0"
tmux split-window -v -t "$SESSION_NAME:0.0"
tmux split-window -v -t "$SESSION_NAME:0.1"

tmux split-window -v -t "$SESSION_NAME:0.0" -p 20

tmux select-pane -t "$SESSION_NAME:0.0" -T "Node 1"
tmux select-pane -t "$SESSION_NAME:0.1" -T "Node 2"
tmux select-pane -t "$SESSION_NAME:0.2" -T "Node 3"
tmux select-pane -t "$SESSION_NAME:0.3" -T "Node 4"
tmux select-pane -t "$SESSION_NAME:0.4" -T "Commands"

declare -a nodes=(
  "node1:8080:7000"
  "node2:8081:7001"
  "node3:8082:7002"
  "node4:8083:7003"
)

echo -e "${YELLOW}Starting bootstrap node...${NC}"
IFS=':' read -r id http_port raft_port <<< "${nodes[0]}"
bootstrap_cmd="go run main.go -id $id -http :$http_port -raft 127.0.0.1:$raft_port -data $DATA_DIR -bootstrap -console-level info -file-level debug -logs $LOG_DIR"
tmux send-keys -t "$SESSION_NAME:0.0" "echo -e \"${CYAN}Starting bootstrap node: $id${NC}\"" C-m
tmux send-keys -t "$SESSION_NAME:0.0" "$bootstrap_cmd" C-m

echo "Waiting for bootstrap node to start..."
sleep "$DELAY_BETWEEN_NODES"

for i in {1..3}; do
  echo -e "${YELLOW}Starting follower node $((i+1))...${NC}"
  IFS=':' read -r id http_port raft_port <<< "${nodes[$i]}"
  follower_cmd="go run main.go -id $id -http :$http_port -raft 127.0.0.1:$raft_port -data $DATA_DIR -join 127.0.0.1:8080 -console-level info -file-level debug -logs $LOG_DIR"
  tmux send-keys -t "$SESSION_NAME:0.$i" "echo -e \"${CYAN}Starting follower node: $id${NC}\"" C-m
  tmux send-keys -t "$SESSION_NAME:0.$i" "$follower_cmd" C-m
  sleep "$DELAY_BETWEEN_NODES"
done

tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${CYAN}Raft3D Test Commands:${NC}\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}1. Check cluster status:${NC} curl http://localhost:8080/cluster/status\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}2. Add a printer:${NC} curl -X POST -H \\\"Content-Type: application/json\\\" -d '{\\\"id\\\":\\\"printer1\\\",\\\"company\\\":\\\"Prusa\\\",\\\"model\\\":\\\"MK3S+\\\"}' http://localhost:8080/api/v1/printers\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}3. Get all printers:${NC} curl http://localhost:8080/api/v1/printers\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}4. Add a filament:${NC} curl -X POST -H \\\"Content-Type: application/json\\\" -d '{\\\"id\\\":\\\"filament1\\\",\\\"type\\\":\\\"PLA\\\",\\\"color\\\":\\\"Red\\\",\\\"total_weight_in_grams\\\":1000,\\\"remaining_weight_in_grams\\\":1000}' http://localhost:8080/api/v1/filaments\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}5. Add a print job:${NC} curl -X POST -H \\\"Content-Type: application/json\\\" -d '{\\\"id\\\":\\\"job1\\\",\\\"printer_id\\\":\\\"printer1\\\",\\\"filament_id\\\":\\\"filament1\\\",\\\"filepath\\\":\\\"/models/benchy.gcode\\\",\\\"print_weight_in_grams\\\":15}' http://localhost:8080/api/v1/print_jobs\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}6. Update job status:${NC} curl -X POST \\\"http://localhost:8080/api/v1/print_jobs/job1/status?status=Running\\\"\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}7. Manually remove a node:${NC} curl -X DELETE \\\"http://localhost:8080/cluster/servers?id=node3\\\"\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}8. Kill a node (simulate failure):${NC} tmux send-keys -t \\\"$SESSION_NAME:0.2\\\" C-c\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${YELLOW}9. Exit:${NC} tmux kill-session -t $SESSION_NAME\"" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo" C-m
tmux send-keys -t "$SESSION_NAME:0.4" "echo -e \"${CYAN}Ready to test commands. Type a command and press Enter:${NC}\"" C-m

echo -e "${GREEN}Raft3D cluster is running!${NC}"
echo "Use the bottom pane to run test commands."
echo "To detach from tmux: press Ctrl+B, then D"
echo "To kill the session: press Ctrl+B, then : followed by 'kill-session'"
tmux attach-session -t "$SESSION_NAME" 