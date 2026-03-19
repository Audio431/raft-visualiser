package raft

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	pb "raft-visualiser/proto"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RaftState int

type RaftServer interface {
	RequestVote(context.Context, *pb.RequestVoteArgs) (*pb.RequestVoteReply, error)
}

const (
	Follower RaftState = iota
	Candidate
	Leader
	Dead
)

const (
	electionTimeoutMin = 150 // ms
	electionTimeoutMax = 300 // ms
	heartbeatMs        = 50  // ms; must be well below election timeout floor
)

func (s RaftState) String() string {
	return [...]string{"Follower", "Candidate", "Leader", "Dead"}[s]
}

// -- Main considerations during Leader Election --

// Premise: any server may increment its own term, vote for itself, and send RequestVote RPCs to all peers in parallel.
// This recurs until one of the following is satisfied:
// 1. A server wins an election.
// 2. Another server wins an election and assumes leadership.
// 3. No winner emerges within the election period.

// How an election proceeds in three scenarios:
// Win by majority: each server grants at most one vote per term, first-come-first-served. A candidate that receives majority votes prevails, then sends heartbeats to assert authority and prevent further elections.

// Lose to another leader: while awaiting RequestVote replies, a candidate may receive an AppendEntries RPC from a server claiming leadership. The decision hinges on term comparison — if the candidate's term exceeds the claimant's, it rejects; otherwise it steps down to follower.

// No winner: if every server becomes a candidate and none secures a majority, votes split — known as "split vote" — leaving all candidates unable to prevail. They time out and start a new election round. The premise still holds, but this is not a definitive solution and may recur indefinitely.

// Raft addresses this by using randomised election timeouts within a fixed interval, so one server's timer fires first and establishes leadership before the remaining servers time out.

type Raft struct {
	mu sync.Mutex

	// Persistent State
	CurrentTerm int            // latest term server has seen (initialised to 0 at start)
	VotedFor    int            // candidateId that received vote in current term (-1 if none)
	Log         []*pb.LogEntry // log entries (First index is 1)

	// Volatile State
	CommitIndex int // index of highest log entry known to be committed
	LastApplied int // index of highest log entry applied to state machine

	// Volatile State on Leaders; Reinitialized after election
	NextIndex  map[int]int // for each server, index of the next log entry to send to that server
	MatchIndex map[int]int // for each server, index of highest log entry known to be replicated on server

	// Node identity and state
	Id       int
	State    RaftState
	peersIds []int

	// gRPC Clients and Server
	peerClients map[int]pb.RaftClient
	grpcServer  *grpc.Server
	pb.UnimplementedRaftServer

	// For redirecting clients to current leader
	leaderId int

	heartbeatCh chan bool
}

// Constructor
func MakeRaft(id int, peerIds []int, listenAddr string, peerAddrs map[int]string) *Raft {
	rf := &Raft{
		Id:          id,
		peersIds:    peerIds,
		State:       Follower,
		Log:         []*pb.LogEntry{},
		VotedFor:    -1,
		CurrentTerm: 0,
		heartbeatCh: make(chan bool),
		peerClients: make(map[int]pb.RaftClient),
	}

	lis, err := net.Listen("tcp", listenAddr)

	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
	}

	rf.grpcServer = grpc.NewServer()
	pb.RegisterRaftServer(rf.grpcServer, rf)

	go rf.grpcServer.Serve(lis)

	for _, pid := range peerIds {
		if pid == id {
			continue
		}

		conn, err := grpc.NewClient(peerAddrs[pid], grpc.WithTransportCredentials(insecure.NewCredentials()))

		if err != nil {
			log.Fatalf("Node %d failed to connect to %d: %v", id, pid, err)
		}
		rf.peerClients[pid] = pb.NewRaftClient(conn)
	}

	// Delay election loop to let all nodes start
	go func() {
		time.Sleep(5 * time.Second)
		rf.termClock()
	}()

	return rf
}

func (rf *Raft) termClock() {

	getTimeout := func() time.Duration {
		// Random election timeout in [150, 300)ms to avoid split votes
		ms := electionTimeoutMin + (rand.Int63() % (electionTimeoutMax - electionTimeoutMin))
		return time.Duration(ms) * time.Millisecond
	}

	// Coupled periodic timers: heartbeat resets the election timer, maintaining leader authority.
	// Absence of heartbeat within the election period triggers a new election.

	electionTimeout := time.NewTimer(getTimeout())
	heartbeatInterval := time.NewTimer(heartbeatMs * time.Millisecond)

	for {
		select {
		case <-rf.heartbeatCh: // Heartbeat received; Leader is alive
			if !electionTimeout.Stop() {
				select {
				case <-electionTimeout.C: // drain stale timer event
				default:
				}
			}
			electionTimeout.Reset(getTimeout())

		case <-electionTimeout.C: // Election timeout
			if rf.State != Leader {
				fmt.Println("Time out: Election start")
				rf.startElection()
			}
			electionTimeout.Reset(getTimeout())

		case <-heartbeatInterval.C: // Leader sends heartbeat
			rf.mu.Lock()
			if rf.State == Leader {
				go rf.broadcastHeartbeat()
			}
			heartbeatInterval.Reset(heartbeatMs * time.Millisecond)
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) startElection() {

	// Vote for self and increment term
	rf.mu.Lock()
	rf.State = Candidate
	rf.CurrentTerm += 1
	rf.VotedFor = rf.Id

	savedTerm := rf.CurrentTerm
	savedId := rf.Id
	votesReceived := 1
	rf.mu.Unlock()

	for _, peerId := range rf.peersIds {
		if peerId == rf.Id {
			continue
		}

		go func(targetId int) {
			args := &pb.RequestVoteArgs{
				Term:         int32(savedTerm), // Ask for other nodes' votes in current term
				CandidateId:  int32(savedId),
				LastLogIndex: 0,
				LastLogTerm:  0,
			}

			reply, err := rf.peerClients[targetId].RequestVote(context.Background(), args)

			if err != nil {
				log.Printf("Node %d failed to request vote from %d: %v", rf.Id, targetId, err)
				return
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()

			// Current Term may change due to AppendEntries from valid Leader
			if rf.State != Candidate || rf.CurrentTerm != savedTerm {
				return
			}

			if reply.Term > int32(rf.CurrentTerm) {
				rf.CurrentTerm = int(reply.Term)
				rf.VotedFor = -1
				rf.State = Follower
				return
			}

			if reply.VoteGranted {
				votesReceived++
				if votesReceived > (len(rf.peersIds))/2 { // majority votes received; 2f + 1 wheres f stands for max faulty nodes
					// Prevent resent the entire log on every heartbeat; only send new entries after election
					rf.State = Leader

					rf.NextIndex = make(map[int]int)
					rf.MatchIndex = make(map[int]int)

					for _, pid := range rf.peersIds {
						rf.NextIndex[pid] = len(rf.Log)
						rf.MatchIndex[pid] = 0
					}

					rf.leaderId = rf.Id
					go rf.broadcastHeartbeat()
					return
				}
			}

		}(peerId)
	}
}

func (rf *Raft) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteReply, error) {
	reply := &pb.RequestVoteReply{}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term > int32(rf.CurrentTerm) {
		rf.CurrentTerm = int(args.Term)
		rf.State = Follower
		rf.VotedFor = -1
	}

	if args.Term < int32(rf.CurrentTerm) {
		reply.VoteGranted = false
		return reply, nil
	}

	reply.Term = int32(rf.CurrentTerm)

	if rf.VotedFor == -1 || rf.VotedFor == int(args.CandidateId) {
		rf.VotedFor = int(args.CandidateId)
		reply.VoteGranted = true
		select {
		case rf.heartbeatCh <- true:
		default:
		}
	} else {
		reply.VoteGranted = false
	}

	return reply, nil
}

// Implement Interface for Network
func (rf *Raft) GetId() int { return rf.Id }

func (rf *Raft) GetState() (RaftState, int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.State, rf.CurrentTerm
}

func (rf *Raft) broadcastHeartbeat() {
	rf.mu.Lock()
	savedTerm := rf.CurrentTerm
	savedTermId := rf.Id
	rf.mu.Unlock()

	for _, peerId := range rf.peersIds {
		if peerId == rf.Id {
			continue
		}
		go func(targetId int) {
			nextIndex := rf.NextIndex[targetId]

			args := &pb.AppendEntriesArgs{Term: int32(savedTerm), LeaderId: int32(savedTermId)}
			args.Entries = rf.Log[nextIndex:]
			args.PrevLogIndex = int32(nextIndex - 1)

			if reply, err := rf.peerClients[targetId].AppendEntries(context.Background(), args); err != nil {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				// If reply.Term > currentTerm, convert to Follower
				if reply.Term > int32(rf.CurrentTerm) {
					rf.CurrentTerm = int(reply.Term)
					rf.State = Follower
					rf.VotedFor = -1
				}
			} else {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				rf.NextIndex[targetId] = nextIndex + len(args.Entries)
				rf.MatchIndex[targetId] = rf.NextIndex[targetId] - 1
			}

		}(peerId)
	}
}

func (rf *Raft) Kill() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.State = Dead
	fmt.Printf("Node %d killed\n", rf.Id)
}

func (rf *Raft) AppendEntries(ctx context.Context, args *pb.AppendEntriesArgs) (*pb.AppendEntriesReply, error) {
	reply := &pb.AppendEntriesReply{}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Reject AppendEntries from older term of leader
	if args.Term < int32(rf.CurrentTerm) {
		reply.Term = int32(rf.CurrentTerm)
		reply.Success = false
		return reply, nil
	}

	if args.Term > int32(rf.CurrentTerm) {
		rf.CurrentTerm = int(args.Term)
		rf.VotedFor = -1
		rf.State = Follower
	}

	if args.Term == int32(rf.CurrentTerm) {
		rf.State = Follower
	}

	for i, entry := range args.Entries {
		index := int(args.PrevLogIndex) + 1 + i
		if index < len(rf.Log) {
			rf.Log[index] = entry // overwrite conflicting entry
		} else {
			rf.Log = append(rf.Log, entry)
		}
	}

	rf.leaderId = int(args.LeaderId)

	reply.Success = true
	select {
	case rf.heartbeatCh <- true:
	default:
	}

	return reply, nil
}

func (rf *Raft) SubmitCommand(ctx context.Context, args *pb.SubmitCommandArgs) (*pb.SubmitCommandReply, error) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.State != Leader {
		return &pb.SubmitCommandReply{Success: false, LeaderId: int32(rf.leaderId)}, nil
	} else {
		rf.Log = append(rf.Log, &pb.LogEntry{Term: int32(rf.CurrentTerm), Command: args.Command})
	}

	return &pb.SubmitCommandReply{Success: true, LeaderId: int32(rf.Id)}, nil
}
