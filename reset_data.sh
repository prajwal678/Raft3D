#!/bin/bash

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

DATA_DIR="./data/"
LOGS_DIR="./logs/"

clean_data_directory() {
    local dir=$1
    echo -e "${YELLOW}Cleaning directory: ${dir}${NC}"
    
    echo -e "Checking for running Raft nodes..."
    if pgrep -f "go run main.go" > /dev/null; then
        echo -e "${RED}Found running Raft nodes. Killing them...${NC}"
        pkill -f "go run main.go"
        sleep 1
    fi
    
    sync
    
    if [ -d "$dir" ]; then
        echo -e "Removing directory: $dir"
        rm -rf "$dir"
    else
        echo -e "Directory $dir doesn't exist. Skipping."
    fi
    
    mkdir -p "$dir"
    echo -e "${GREEN}Created fresh directory: $dir${NC}"
}

echo -e "${GREEN}Resetting Raft3D data...${NC}"

if tmux has-session -t "raft3d" 2>/dev/null; then
    echo -e "${YELLOW}Found tmux session 'raft3d'. Killing it...${NC}"
    tmux kill-session -t "raft3d"
    sleep 1
fi

clean_data_directory $DATA_DIR
clean_data_directory $LOGS_DIR

sync

echo -e "${GREEN}Data reset complete. You can now start a fresh Raft cluster.${NC}"
echo -e "Run: ${YELLOW}./run_raft_cluster.sh${NC}" 
