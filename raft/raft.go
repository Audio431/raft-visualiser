package raft

import (
	"fmt"
	"math/rand"
	"raft-visualiser/network"
	"sync"
	"time"
)

type RaftState int

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
	CurrentTerm int        // latest term server has seen (initialised to 0 at start)
	VotedFor    int        // candidateId that received vote in current term (-1 if none)
	Log         []LogEntry // log entries (First index is 1)

	// Volatile State
	CommitIndex int // index of highest log entry known to be committed
	LastApplied int // index of highest log entry applied to state machine

	// Volatile State on Leaders; Reinitialized after election
	NextIndex  map[int]int // for each server, index of the next log entry to send to that server
	MatchIndex map[int]int // for each server, index of highest log entry known to be replicated on server

	// Additional Fields to Support Simulation
	Id          int
	State       RaftState
	peersIds    []int
	net         *network.Network
	heartbeatCh chan bool
}

type LogEntry struct {
	Term    int
	Command interface{}
}

type AppendEntriesArgs struct {
	Term         int // leader’s term
	LeaderId     int // let followers redirect clients
	PrevLogIndex int // index of log entry immediately preceding new ones

	PrevLogTerm  int        // term of prevLogIndex entry
	Entries      []LogEntry // log entries to store (empty for heartbeat; may send more than one for efficiency)
	LeaderCommit int        // leader’s commitIndex
}

type AppendEntriesReply struct {
	Term    int  // currentTerm, for leader to update itself
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}

type RequestVoteArgs struct {
	Term         int // candidate’s term
	CandidateId  int // candidate requesting vote
	LastLogIndex int // index of candidate’s last log entry
	LastLogTerm  int // term of candidate’s last log entry
}

type RequestVoteReply struct {
	Term        int  // currentTerm, for candidate to update itself
	VoteGranted bool // true means candidate received vote
}

// Constructor
func MakeRaft(id int, peerIds []int, net *network.Network) *Raft {
	rf := &Raft{
		Id:          id,
		peersIds:    peerIds,
		net:         net,
		State:       Follower,
		VotedFor:    -1,
		CurrentTerm: 0,
		heartbeatCh: make(chan bool),
	}

	net.Register(rf)

	go rf.termLoop()

	return rf
}

// termLoop manages the passage of time in Raft.
// Since time is divided into terms, this loop handles the lifecycle of each term (elections and heartbeats).

func (rf *Raft) termLoop() {

	timeoutDuration := time.Duration(500+rand.Intn(500)) * time.Millisecond
	electionTimer := time.NewTimer(timeoutDuration)
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
			electionTimer.Reset(time.Duration(500+rand.Intn(500)) * time.Millisecond)

		case <-electionTimer.C: // Election timeout
			rf.startElection()
			electionTimer.Reset(time.Duration(500+rand.Intn(500)) * time.Millisecond)
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
			args := RequestVoteArgs{
				Term:         savedTerm, // Ask for other nodes' votes in current term
				CandidateId:  savedId,
				LastLogIndex: 0,
				LastLogTerm:  0,
			}
			reply := RequestVoteReply{}

			if rf.net.Call(targetId, "RequestVote", &args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				// Current Term may change due to AppendEntries from valid Leader
				if rf.State != Candidate || rf.CurrentTerm != savedTerm {
					return
				}

				if reply.VoteGranted {
					votesReceived++
					if votesReceived > (len(rf.peersIds))/2 {
						rf.State = Leader
						go rf.broadcastHeartbeat()
						return
					}
				}
			}
		}(peerId)
	}
}

func (rf *Raft) ReceiveRequestVote(args interface{}, reply interface{}) {
	// Casting Interface
	realArgs := args.(*RequestVoteArgs)
	realReply := reply.(*RequestVoteReply)

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if realArgs.Term > rf.CurrentTerm {
		rf.CurrentTerm = realArgs.Term
		rf.State = Follower
		rf.VotedFor = -1
	}

	// Reject RequestVote from older term of candidate
	if realArgs.Term < rf.CurrentTerm {
		realReply.VoteGranted = false
		return
	}

	realReply.Term = rf.CurrentTerm

	// Accept RequestVote from current or newer term of candidate
	if rf.VotedFor == -1 || rf.VotedFor == realArgs.CandidateId {
		rf.VotedFor = realArgs.CandidateId
		realReply.VoteGranted = true

		select {
		case rf.heartbeatCh <- true:
		default:
		}

		return
	} else {
		realReply.VoteGranted = false
		return
	}

}

// Implement Interface for Network
func (rf *Raft) GetId() int { return rf.Id }

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
			args := AppendEntriesArgs{Term: savedTerm, LeaderId: savedTermId}
			reply := AppendEntriesReply{}
			if rf.net.Call(targetId, "AppendEntries", &args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				// If reply.Term > currentTerm, convert to Follower
				if reply.Term > rf.CurrentTerm {
					rf.CurrentTerm = reply.Term
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

func (rf *Raft) ReceiveAppendEntries(args interface{}, reply interface{}) {
	// Casting Interface
	realArgs := args.(*AppendEntriesArgs)
	realReply := reply.(*AppendEntriesReply)

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Reject AppendEntries from older term of leader
	if realArgs.Term < rf.CurrentTerm {
		realReply.Term = rf.CurrentTerm
		realReply.Success = false
		return
	}

	// Accept AppendEntries from current or newer term of leader
	if (realArgs.Term > rf.CurrentTerm) || (realArgs.Term == rf.CurrentTerm && rf.State == Candidate) {
		rf.CurrentTerm = realArgs.Term
		rf.VotedFor = -1
		rf.State = Follower
	}

	realReply.Success = true

	// Reset Election Timer
	select {
	case rf.heartbeatCh <- true:
	default:
	}
}
