package network

import (
	"math/rand"
	"sync"
	"time"
)

type RaftAPI interface {
	ReceiveAppendEntries(args interface{}, reply interface{})
	ReceiveRequestVote(args interface{}, reply interface{})
	GetId() int
}

type Network struct {
	mu    sync.Mutex
	nodes map[int]RaftAPI
}

func MakeNetwork() *Network {
	return &Network{nodes: make(map[int]RaftAPI)}
}

func (net *Network) Register(node RaftAPI) {
	net.mu.Lock()
	defer net.mu.Unlock()
	net.nodes[node.GetId()] = node
}

func (net *Network) Call(toID int, method string, args interface{}, reply interface{}) bool {
	// 1. Sim Delay
	time.Sleep(time.Duration(10+rand.Intn(20)) * time.Millisecond)

	// 2. Sim Drop (10%)
	if rand.Float32() < 0.1 {
		return false
	}

	net.mu.Lock()
	target, ok := net.nodes[toID]
	net.mu.Unlock()

	if !ok {
		return false
	}

	// 3. Forward Call
	switch method {
	case "AppendEntries":
		target.ReceiveAppendEntries(args, reply)
		return true
	case "RequestVote":
		target.ReceiveRequestVote(args, reply)
		return true
	}
	return false
}
