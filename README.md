# Distributed Systems Project: DFSHA

DFSHA is a distributed file-storage project with a MetaNode that manages file and block metadata and DataNodes that store block contents on local disks.

## Run the project

Run each server in a separate terminal from the repository root. Keep the terminals open to see each component's logs.

### MetaNode (Windows PowerShell)

```powershell
go run .\cmd\metanode
```

### DataNode (Windows PowerShell)

Configure a unique ID, listening address, advertised address, storage directory, and the required secrets for each DataNode. Example:

```powershell
$env:DFSHA_DATANODE_ID = "datanode-1"
$env:DFSHA_DATANODE_ADDR = ":9000"
$env:DFSHA_DATANODE_ADVERTISE_ADDR = "localhost:9000"
$env:DFSHA_DATANODE_DIR = ".\data\datanode-1"
$env:DFSHA_METANODE_URL = "http://localhost:8080"
go run .\cmd\datanode
```

Open another terminal for each additional DataNode and use a different ID, port, and data directory. Set the secrets required by your configuration before starting the processes.

### Client (Windows PowerShell)

```powershell
go run .\cmd\client
```

### Linux

Use the equivalent Go commands, for example:

```bash
go run ./cmd/metanode
go run ./cmd/datanode
go run ./cmd/client
```

Run each long-lived process in its own terminal. Configure different ports and data directories for additional DataNodes.

## Metadata persistence and DataNode reconciliation

- State snapshots, HA lock/heartbeat files, and block writes use OS-specific replacement semantics. On Windows, `MoveFileExW` safely replaces an existing file; this supports repeated updates and retries on both Windows and POSIX systems.

- The MetaNode persists its state to `state.json` and restores it on startup when the file already exists.
- In single-node mode, the default state path is `data/MetaNode/<node-id>/state.json`. `METANODE_DATA_DIR` changes the default directory, while `METANODE_STATE_PATH` specifies an exact file path.
- When `METANODE_SHARED_DIR` is configured for shared-state/high-availability mode, the default state path is `<METANODE_SHARED_DIR>/state.json`, unless `METANODE_STATE_PATH` overrides it.
- Persisted state includes the file and block catalog, users, directories, registered DataNodes, their status, and pending block deletions.
- Each DataNode stores block contents in its configured local data directory. Do not delete that directory if its blocks must be retained.
- DataNodes report their on-disk block inventory to the MetaNode. The MetaNode compares that inventory with the current file catalog and schedules stale or orphaned blocks for deletion.
- Deletion requests remain in the persisted queue until a later inventory confirms that the block is absent. This allows the deletion workflow to resume after a MetaNode or DataNode restart.
- If a DataNode is replaced with an empty disk, its missing replicas are no longer advertised as present. This reconciliation does not, by itself, reconstruct lost block contents or guarantee automatic re-replication; a separate repair/replication workflow is required for that.

**Important:** use the same state path and DataNode storage directories across restarts. Configure matching cluster/capability secrets on the participating components. Use strong, private secrets outside local development.

## Scripts

### `scripts/empty_database.py`

Reset the persisted MetaNode metadata on Windows, Linux, or macOS without manually deleting DataNode storage directories. Stop **all MetaNodes** before running this script. DataNodes may remain running; when they reconnect, they report their disk inventories and the empty MetaNode catalog causes unreferenced blocks to be queued for deletion. The DataNodes remove those files through the normal authenticated heartbeat workflow. This frees filesystem space without secure-erasing the underlying sectors.

Run a preview first from the repository root:

```powershell
python .\scripts\empty_database.py --dry-run
```

```bash
python3 ./scripts/empty_database.py --dry-run
```

Then run the reset and type `RESET DFSHA` when prompted:

```powershell
python .\scripts\empty_database.py
```

```bash
python3 ./scripts/empty_database.py
```

The script backs up every state/coordination file it removes. It never deletes DataNode block directories. It uses `METANODE_STATE_PATH`, `METANODE_SHARED_DIR`, `METANODE_DATA_DIR`, and `METANODE_ID` when they are available in the script's environment, with the same precedence as the MetaNode. If the server was started with a path configured in a different shell, pass that exact path explicitly, for example `--state-path ./data/MetaNode/MetaNode-1/state.json`. For shared HA mode, the script also resets known lock and heartbeat files unless `--keep-ha-coordination` is supplied.

A reset deletes users, directory entries, file metadata, block locations, and pending deletion commands from the old catalog. Users must register again. Retain DataNode directories: stale block files are logically freed by being removed after the MetaNode confirms that no current file references them. Do not run the reset while any MetaNode is active, because it could recreate the old database from in-memory state.

## Basic project structure

```text
dfsha/
├── cmd/
│   ├── client/
│   ├── datanode/
│   └── metanode/
├── internal/
├── proto/
└── scripts/
    └── empty_database.py
```

Code is organized into packages by responsibility under `internal`.

## Generated gRPC code

The gRPC contracts live in `proto/`, and the generated Go code is version-controlled under `internal/proto/`. Therefore, only Go is required to build and run the project; the generated code is already included.

```powershell
go build ./...
```

### Regenerate code (only after editing a `.proto` file)

Anyone who modifies a `.proto` file must regenerate and commit the generated output. The project uses [buf](https://buf.build) with the Go plugins. Install the tools once:

```powershell
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Generate the code from the repository root:

```powershell
buf generate proto
```

The plugin binaries are installed in Go's binary directory (`go env GOPATH\bin`), which must be available on `PATH`.
