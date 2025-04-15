Raft3D - 3D Prints
Management using Raft Consensus Algorithm

Introduction

- The project requires development of backend API endpoints for a Distributed 3D printer management system with data persistence implemented via the Raft Consensus Algorithm rather than traditional centralized databases
- Data consistency across the Raft3D network must be maintained through the Raft Consensus mechanism
- Demonstration requires a minimum of three operational nodes (implementable as separate terminals or containers)
- Project demonstration must effectively showcase core Raft functionality including Leader Election and Data Persistence during node failure
- Successful implementation of Raft Consensus across minimal endpoints takes precedence over comprehensive API development
- Projects demonstrating proper Raft fault tolerance and leader election will receive higher evaluation scores than those with complete API implementations but inadequate Raft functionality
- Development priorities should be established accordingly, with primary focus on correct Raft implementation

Technologies/Languages to be used
- You are free to use any language such as Python, Go, Java, among others.
- Ensure that the chosen language supports all the required functionality.
- Implementation requires a Raft library that necessitates manual development of the Raft Finite State Machine (FSM) State Transition Function, such as openraft or hashicorp/raft. Higher-level abstraction libraries such as PyObjSync are expressly prohibited for this implementation.
- My personal recommendation is to use hashicorp/raft (https://github.com/hashicorp/raft)
which uses Go as the language
- Given below is a list of libraries that you are permitted to use grouped by language and arranged in a loose order based on my level of recommendation for each:
Golang: hashicorp/raft (https://github.com/hashicorp/raft), dragonboat
(https://github.com/lni/dragonboat), etcd-io/raft (https://github.com/etcd-io/raft)
Rust: openraft (https://github.com/databendlabs/openraft), async-raft
(https://github.com/databendlabs/openraft)
C++: ebay/NuRaft (https://github.com/eBay/NuRaft),
Erlang: rabbitmq/ra (https://github.com/rabbitmq/ra)

Project Specification

Raft Node
Must handle the following (going above and beyond these are upto the developer)
- Leader election : selecting the leader for the KRaft cluster
- Event driven architecture
- Eventual Consistency
- Failover management : must be able to provide standard raft failover guarantees (3 node cluster can handle single failure; 5 node cluster can handle 2 failures;)
- Maintaining event log : event log of all changes being made (this can be used to reconstruct the metadata store)
- Snapshotting : Creation and retrieval of the snapshots. Take periodic snapshots of the event log at the leader to be able to provide a level of fault tolerance

NOTE : Most of the above are inherently supported by raft as a consensus algorithm; Implementation requires a Raft library that necessitates manual development of the Raft Finite State Machine (FSM) State Transition Function, such as pyraft or hashicorp/raft. Higher-level abstraction libraries such as PyObjSync are expressly prohibited for this implementation. Refer above for permitted Libraries

3D Print Management Software
Raft3D must implement HTTP REST API endpoints accessible to external clients, satisfying the requirements specified below, in addition to maintaining inter-node communication for Raft consensus operations.

Objects Stored
The following objects need to be stored by Raft3D (and be made raft fault tolerant, this will be the state in your RAFT FSM)

1. Printers - The Individual 3D printers present in the shop
  {
    "id": "", //can be int or string but must be unique for every printer
    "company": "", //type: string; eg: Creality, Prussa etc
    "model": "", //type: string; eg: Ender 3, i3 MK3S+
  }

2. Filaments - The rolls of plastic filament used for 3D printing parts
  {
    "id": "", //can be int or string but must be unique for every filament
    "type": "", //type:string; options: PLA, PETG, ABS, TPU
    "color": "", //type: string; eg: red, blue, black etc
    "total_weight_in_grams": "", //type: int
    "remaining_weight_in_grams": "", //type: int
  }

- type is the type of plastic filament which can be one of the following options: PLA, PETG, ABS or TPU
- color is the color of the plastic
- total_weight_in_grams is the weight of usable plastic on the filament roll when it comes from the factory, usually 1Kg
- remaining_weight_in_grams is the remaining weight of plastic left on the filament roll. This will need to be reduced after every print is DONE based on the weight of print.

3. PrintJobs - A job to print individual items on a particular 3d printer using a particular filament
  {
    "id": "", //can be int or string but must be unique for every filament
    "type": "", //type:string; options: PLA, PETG, ABS, TPU
    "color": "", //type: string; eg: red, blue, black etc
    "total_weight_in_grams": "", //type: int
    "remaining_weight_in_grams": "", //type: int
  }

- filepath - The path of the 3D file that will be printed (usually in .gcode file format)
- print_weight_in_grams - The weight of the completed print in grams. This cannot exceed the remaining weight of the chosen filament
- status - Can be one for the following:
  - Queued - Default on Creation
  - Running - Print is running on the Printer
  - Done - Print is done
  - Canceled - Print was Canceled (during either Queues or Running State)

API Spec
The following endpoints need to be supported:

1. POST printer (e.g: POST /api/v1/printers)
- Create a Printer

2. GET printers (e.g: GET /api/v1/printers)
- List all available printers with their details

3. POST filament (e.g: POST /api/v1/filaments)
- Create a filament

4. GET filaments (e.g: GET /api/v1/filaments)
- List all available filaments with their details

5. POST Print Job (e.g: POST /api/v1/print_ jobs)
- Create a Print Job
- Make Sure printer_id and filament_id are valid and belong to existing printers/filaments
- Make sure that the print_weight_in_grams does not exceed ( remaining_weight_in_grams of the filament MINUS print_weight_in_grams of other Queued/Running Print Jobs that use the same filament)
- Initialize status to Queued. Do not let user set the status while creating Print Job

6. GET Print Jobs (e.g: GET /api/v1/print_ jobs)
- List all print jobs with their details
- You may optionally allow for filtering based on status if you are feeling 

7. Update Print Job Status (e.g: POST /api/v1/print_ jobs/<job_id>/status?status="running")
- Update the status of a Print Job by taking in its ID and the next status which can be either running, done or canceled
- A job can go to running state only from the queued state
- A job can go to done state only from the running state
- A job can go to canceled state from either queued state or running state
- No other transitions apart from the ones specified above are allowed
- When a job transitions to done state, you need to reduce the remaining_weight_in_grams of its filament by the print_weight_in_grams of the current job
