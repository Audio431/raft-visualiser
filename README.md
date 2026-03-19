# Raft Consensus Algorithm Visualiser

The Raft consensus algorithm, as specified in [Ongaro & Ousterhout (2014)](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf), is designed for understandability — yet most available implementations simplify or omit portions of the specification in favour of demonstration. This project takes the opposite approach: a precise implementation of the extended paper, conducted alongside the [MIT 6.5840 Distributed Systems](https://pdos.csail.mit.edu/6.824/) course (formerly 6.824), with a visualisation layer for real-time inspection of cluster behaviour.

The system comprises two components:
- **Raft cluster** — Go nodes communicating via gRPC, following the paper's RPC definitions (`RequestVote`, `AppendEntries`, `InstallSnapshot`). Instrumented with OpenTelemetry Go SDK for distributed tracing across all inter-node RPCs.
- **Visualiser** — React frontend connected to the cluster over gRPC, rendering cluster state transitions as they occur. Instrumented with OpenTelemetry JS SDK. Telemetry from both layers is aggregated through an OTEL Collector.

## Roadmap

Implementation follows the paper's structure. Each phase corresponds to a section of the specification.

### Raft Cluster (Go + gRPC + OTEL)
- [ ] Leader election — term management, RequestVote RPC, randomised election timeouts, split vote resolution
- [ ] Log replication — AppendEntries RPC, log consistency check, commitment by majority
- [ ] Safety — election restriction (§5.4.1), commitment rules for entries from prior terms (§5.4.2)
- [ ] Persistence — stable storage of currentTerm, votedFor, and log entries across restarts
- [ ] Log compaction — InstallSnapshot RPC, state machine snapshotting (§7)
- [ ] Cluster membership changes — joint consensus for configuration transitions (§6)
- [ ] OTEL instrumentation — tracing across all RPCs and state transitions

### Visualisation (React + OTEL)
- [ ] Cluster topology — node states (follower, candidate, leader), current term, leader identity
- [ ] Log replication — per-node log state, commit index progression
- [ ] Election sequence — vote requests, grants, term transitions, timeout events
- [ ] OTEL JS SDK integration — end-to-end trace correlation with the cluster layer

## References

- [In Search of an Understandable Consensus Algorithm (Extended Version)](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf) — Ongaro & Ousterhout, 2014
- [MIT 6.5840 Distributed Systems](https://pdos.csail.mit.edu/6.824/)
