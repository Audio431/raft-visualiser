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

func (s RaftState) String() string {
	return [...]string{"Follower", "Candidate", "Leader", "Dead"}[s]
}

// --- Main Struct --- cited from In Search of an Understandable Consensus Algorithm (Extended Version) ---
// https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf

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

	heartbeatCh chan bool
}

// Constructor
func MakeRaft(id int, peerIds []int, listenAddr string, peerAddrs map[int]string) *Raft {
	rf := &Raft{
		Id:          id,
		peersIds:    peerIds,
		State:       Follower,
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
		time.Sleep(2 * time.Second)
		rf.termLoop()
	}()

	return rf
}

// termLoop manages the passage of time in Raft.
// Since time is divided into terms, this loop handles the lifecycle of each term (elections and heartbeats).

func (rf *Raft) termLoop() {

	getTimeout := func() time.Duration {
		ms := 150 + (rand.Int63() % 150)
		return time.Duration(ms) * time.Millisecond
	}

	electionTimer := time.NewTimer(getTimeout())
	for {
		rf.mu.Lock()
		state := rf.State
		rf.mu.Unlock()

		if state == Leader {
			rf.broadcastHeartbeat()
			time.Sleep(50 * time.Millisecond)
			continue
		}

		select {
		case <-rf.heartbeatCh: // Heartbeat received; Leader is alive
			if !electionTimer.Stop() {
				select {
				case <-electionTimer.C: // Last heartbeat expired ==> timeout
				default:
				}
			}
			electionTimer.Reset(getTimeout())

		case <-electionTimer.C: // Election timeout
			rf.startElection()
			electionTimer.Reset(getTimeout())
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

			if reply.VoteGranted {
				votesReceived++
				if votesReceived > (len(rf.peersIds))/2 { // majority votes received; 2f + 1 wheres f stands for max faulty nodes
					rf.State = Leader
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
			args := &pb.AppendEntriesArgs{Term: int32(savedTerm), LeaderId: int32(savedTermId)}
			if reply, err := rf.peerClients[targetId].AppendEntries(context.Background(), args); err != nil {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				// If reply.Term > currentTerm, convert to Follower
				if reply.Term > int32(rf.CurrentTerm) {
					rf.CurrentTerm = int(reply.Term)
					rf.State = Follower
					rf.VotedFor = -1
				}
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
	// Casting Interface
	reply := &pb.AppendEntriesReply{}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Reject AppendEntries from older term of leader
	if args.Term < int32(rf.CurrentTerm) {
		reply.Term = int32(rf.CurrentTerm)
		reply.Success = false
		return reply, nil
	}

	// Accept AppendEntries from current or newer term of leader
	if (args.Term > int32(rf.CurrentTerm)) || (args.Term == int32(rf.CurrentTerm) && rf.State == Candidate) {
		rf.CurrentTerm = int(args.Term)
		rf.VotedFor = -1
		rf.State = Follower
	}

	reply.Success = true

	// Reset Election Timer
	select {
	case rf.heartbeatCh <- true:
	default:
	}

	return reply, nil
}
