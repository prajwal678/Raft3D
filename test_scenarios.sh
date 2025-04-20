#!/bin/bash

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
NC='\033[0m'

BASE_URL="http://localhost:8080"

run_command() {
  local title="$1"
  local command="$2"
  
  echo -e "\n${YELLOW}=== $title ===${NC}"
  echo -e "${CYAN}Command:${NC} $command"
  echo -e "${CYAN}Response:${NC}"
  eval "$command"
  echo -e "\n${GREEN}Command completed${NC}"
  sleep 1
}

check_cluster_health() {
  echo -e "\n${YELLOW}Checking cluster health...${NC}"
  response=$(curl -s $BASE_URL/cluster/status)
  echo $response | jq -c .
  
  node_count=$(echo $response | jq -c '. | length')
  
  if [[ "$node_count" -lt 1 ]]; then
    echo -e "${RED}Cluster appears to be unhealthy. Not enough nodes.${NC}"
    return 1
  fi
  
  echo -e "${GREEN}Cluster is healthy.${NC}"
  return 0
}

wait_for_leader() {
  echo -e "\n${YELLOW}Waiting for leader election...${NC}"
  max_attempts=10
  attempt=1
  
  while [[ $attempt -le $max_attempts ]]; do
    response=$(curl -s $BASE_URL/cluster/status)
    leader=$(echo $response | jq -r '.leader')
    
    if [[ "$leader" != "" && "$leader" != "null" ]]; then
      echo -e "${GREEN}Leader elected: $leader${NC}"
      return 0
    fi
    
    echo -e "${YELLOW}No leader yet, waiting (attempt $attempt/$max_attempts)...${NC}"
    sleep 3
    ((attempt++))
  done
  
  echo -e "${RED}No leader elected after $max_attempts attempts.${NC}"
  return 1
}

test_basic_workflow() {
  echo -e "\n${YELLOW}=== Running Basic Workflow Test ===${NC}"
  
  run_command "Adding a printer" "curl -s -X POST -H \"Content-Type: application/json\" -d '{\"id\":\"printer1\",\"company\":\"Prusa\",\"model\":\"MK3S+\"}' $BASE_URL/api/v1/printers"
  run_command "Getting all printers" "curl -s $BASE_URL/api/v1/printers | jq -c ."
  run_command "Adding a filament" "curl -s -X POST -H \"Content-Type: application/json\" -d '{\"id\":\"filament1\",\"type\":\"PLA\",\"color\":\"Red\",\"total_weight_in_grams\":1000,\"remaining_weight_in_grams\":1000}' $BASE_URL/api/v1/filaments"
  run_command "Getting all filaments" "curl -s $BASE_URL/api/v1/filaments | jq -c ."
  run_command "Adding a print job" "curl -s -X POST -H \"Content-Type: application/json\" -d '{\"id\":\"job1\",\"printer_id\":\"printer1\",\"filament_id\":\"filament1\",\"filepath\":\"/models/benchy.gcode\",\"print_weight_in_grams\":15}' $BASE_URL/api/v1/print_jobs"
  run_command "Getting all print jobs" "curl -s $BASE_URL/api/v1/print_jobs | jq -c ."
  run_command "Updating job status to running" "curl -s -X POST \"$BASE_URL/api/v1/print_jobs/job1/status?status=Running\""
  run_command "Getting updated print jobs" "curl -s $BASE_URL/api/v1/print_jobs | jq -c ."
  run_command "Updating job status to done" "curl -s -X POST \"$BASE_URL/api/v1/print_jobs/job1/status?status=Done\""
  run_command "Getting final print jobs" "curl -s $BASE_URL/api/v1/print_jobs | jq -c ."
  
  echo -e "\n${GREEN}Basic workflow test completed successfully!${NC}"
}

test_node_failure() {
  echo -e "\n${YELLOW}=== Running Node Failure Test ===${NC}"
  
  run_command "Initial cluster status" "curl -s $BASE_URL/cluster/status | jq -c ."
  # killing node3
  echo -e "\n${YELLOW}Killing node3...${NC}"
  tmux send-keys -t "raft3d:0.2" C-c
  
  # wait to detect the failure
  echo -e "\n${YELLOW}Waiting for cluster to detect the failure...${NC}"
  sleep 15
  
  # status after failure
  run_command "Cluster status after node failure" "curl -s $BASE_URL/cluster/status | jq -c ."
  
  # add a new printer to verify cluster still works
  run_command "Adding a printer after node failure" "curl -s -X POST -H \"Content-Type: application/json\" -d '{\"id\":\"printer2\",\"company\":\"Creality\",\"model\":\"Ender 3\"}' $BASE_URL/api/v1/printers"
  
  # get all printers to verify the operation succeeded
  run_command "Getting all printers after node failure" "curl -s $BASE_URL/api/v1/printers | jq -c ."
  
  # restart the KILLED node lmao resurrection typa shii
  echo -e "\n${YELLOW}Restarting node3...${NC}"
  tmux send-keys -t "raft3d:0.2" "go run main.go -id node3 -http :8082 -raft 127.0.0.1:7002 -data ./data -join 127.0.0.1:8080 -console-level info -file-level debug -logs ./logs" C-m
  
  # wait for the node to rejoin
  echo -e "\n${YELLOW}Waiting for node to rejoin...${NC}"
  sleep 10
  
  # final cluster status
  run_command "Final cluster status" "curl -s $BASE_URL/cluster/status | jq -c ."
  
  # verify the rejoined node has the data
  run_command "Verifying data on rejoined node" "curl -s http://localhost:8082/api/v1/printers | jq -c ."
  
  echo -e "\n${GREEN}Node failure and recovery test completed!${NC}"
}

test_leader_failure() {
  echo -e "\n${YELLOW}=== Running Leader Failure Test ===${NC}"
  
  leader_response=$(curl -s $BASE_URL/cluster/status)
  is_leader=$(echo $leader_response | jq -r '.is_leader')
  
  if [[ "$is_leader" == "true" ]]; then
    echo -e "\n${YELLOW}Current node is the leader. Killing it...${NC}"
    tmux send-keys -t "raft3d:0.0" C-c
    
    # need to change the base URL since we're killing the leader
    BASE_URL="http://localhost:8081"
  else
    leader_id=$(echo $leader_response | jq -r '.leader')
    echo -e "\n${YELLOW}Current leader is $leader_id. Finding and killing it...${NC}"
    
    # find leader's pane and kill it
    for i in {0..3}; do
      pane_resp=$(curl -s http://localhost:$((8080 + i))/cluster/status 2>/dev/null)
      is_leader=$(echo $pane_resp | jq -r '.is_leader' 2>/dev/null)
      
      if [[ "$is_leader" == "true" ]]; then
        echo -e "${YELLOW}Leader found on pane 0.$i. Killing it...${NC}"
        tmux send-keys -t "raft3d:0.$i" C-c
        
        # update base URL to another node
        next_i=$(( (i + 1) % 4 ))
        BASE_URL="http://localhost:$((8080 + next_i))"
        break
      fi
    done
  fi
  
  # wait for a new leader to be elected
  echo -e "\n${YELLOW}Waiting for new leader election...${NC}"
  sleep 15
  
  # new cluster status
  run_command "Cluster status after leader failure" "curl -s $BASE_URL/cluster/status | jq -c ."
  
  # add a new printer to verify cluster still works
  run_command "Adding a printer after leader failure" "curl -s -X POST -H \"Content-Type: application/json\" -d '{\"id\":\"printer3\",\"company\":\"Anycubic\",\"model\":\"Photon\"}' $BASE_URL/api/v1/printers"
  
  # get all printers to verify the operation succeeded
  run_command "Getting all printers after leader failure" "curl -s $BASE_URL/api/v1/printers | jq -c ."
  
  echo -e "\n${GREEN}Leader failure and election test completed!${NC}"
}

echo -e "${GREEN}Starting Raft3D test scenarios...${NC}"

if ! command -v jq &> /dev/null; then
    echo -e "${RED}Error: jq is not installed but required for JSON processing.${NC}"
    echo "Please install jq: sudo apt install jq"
    exit 1
fi

if ! tmux has-session -t "raft3d" 2>/dev/null; then
    echo -e "${RED}Error: tmux session 'raft3d' is not running.${NC}"
    echo "Please start the Raft cluster first with ./run_raft_cluster.sh"
    exit 1
fi

check_cluster_health || exit 1
wait_for_leader || exit 1
echo -e "\n${GREEN}Running test scenarios...${NC}"

echo -e "${CYAN}Available Test Scenarios:${NC}"
echo "1. Basic Workflow Test (add printer, filament, job, update status)"
echo "2. Node Failure and Recovery Test"
echo "3. Leader Failure and Election Test"
echo "4. Run All Tests"
echo "q. Quit"

read -p "Select a test to run (1-4 or q): " choice

case $choice in
    1)
        test_basic_workflow
        ;;
    2)
        test_node_failure
        ;;
    3)
        test_leader_failure
        ;;
    4)
        test_basic_workflow
        test_node_failure
        test_leader_failure
        ;;
    q|Q)
        echo -e "${GREEN}Exiting.${NC}"
        exit 0
        ;;
    *)
        echo -e "${RED}Invalid choice.${NC}"
        exit 1
        ;;
esac

echo -e "\n${GREEN}All tests completed!${NC}" 