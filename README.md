# Raft Consensus Algorithm Visualiser

The Raft consensus algorithm, as specified in [Ongaro & Ousterhout (2014)](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf), is designed for understandability — yet most available implementations simplify or omit portions of the specification in favour of demonstration. This project takes the opposite approach: a precise implementation of the extended paper, conducted alongside the [MIT 6.5840 Distributed Systems](https://pdos.csail.mit.edu/6.824/) course (formerly 6.824), with a visualisation layer for real-time inspection of cluster behaviour.

The system comprises two components:
- **Raft cluster** — Go nodes communicating via gRPC, following the paper's RPC definitions (`RequestVote`, `AppendEntries`, `InstallSnapshot`). Instrumented with OpenTelemetry Go SDK for distributed tracing across all inter-node RPCs.
- **Visualiser** — React frontend connected to the cluster over gRPC, rendering cluster state transitions as they occur. Instrumented with OpenTelemetry JS SDK. Telemetry from both layers is aggregated through an OTEL Collector.

## Progress

**Status: paused** since Mar 28, 2026. Time has gone to an open-source contribution and an AI engineer internship. Work resumes at the OTEL node below.

Dates from git history. `●` done, `◐` in progress, `○` not started.

```
2026-01-22         2026-03-11         2026-03-19         2026-03-28 (paused)
●━━━━━━━━━━━━━━━━━━●━━━━━━━━━━━━━━━━━━◐╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌◐╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌○╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌○╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌○
Leader election    gRPC transport     Log replication    OTEL SDK wired     Safety             RPC spans          Visualiser
                                      (commit pending)   (smoke span only)  §5.4.1 / 5.4.2
```

## Roadmap

Implementation follows the paper's structure. Each phase corresponds to a section of the specification.

### Raft Cluster (Go + gRPC + OTEL)
- [x] Leader election — term management, RequestVote RPC, randomised election timeouts, split vote resolution
- [ ] Log replication — AppendEntries RPC, log consistency check, commitment by majority
  - [x] AppendEntries RPC — leader ships `Log[NextIndex:]` on each heartbeat, `NextIndex`/`MatchIndex` bookkeeping, `SubmitCommand` with leader redirect
  - [ ] Log consistency check — receiver check exists but runs after entries are appended; leader does not yet fill `PrevLogTerm`
  - [ ] Commitment by majority — `CommitIndex`, `LeaderCommit`, `LastApplied` not yet advanced
- [ ] Safety — election restriction (§5.4.1), commitment rules for entries from prior terms (§5.4.2)
- [ ] Persistence — stable storage of currentTerm, votedFor, and log entries across restarts [As a cluster seldomly been restart in this practice project then retain to be a could have]
- [ ] Log compaction — InstallSnapshot RPC, state machine snapshotting (§7) [retain to be a could have requirement]
- [ ] Cluster membership changes — joint consensus for configuration transitions (§6) [retain to be a could have requirement]
- [ ] OTEL instrumentation — tracing across all RPCs and state transitions
  - [x] OTEL Go SDK wired — OTLP/gRPC exporter to the OTEL Collector, per-node `service.name`, flush on shutdown, startup smoke span
  - [ ] Spans on RequestVote / AppendEntries and on state transitions

### Visualisation (React + OTEL)
- [ ] Cluster topology — node states (follower, candidate, leader), current term, leader identity
- [ ] Log replication — per-node log state, commit index progression
- [ ] Election sequence — vote requests, grants, term transitions, timeout events
- [ ] OTEL JS SDK integration — end-to-end trace correlation with the cluster layer

## References

- [In Search of an Understandable Consensus Algorithm (Extended Version)](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf) — Ongaro & Ousterhout, 2014
- [MIT 6.5840 Distributed Systems](https://pdos.csail.mit.edu/6.824/)
