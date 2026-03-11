package main

import (
	"fmt"
	"raft-visualiser/raft"
	"time"
)

func main() {
	n := 5
	peersIds := []int{0, 1, 2, 3, 4}
	nodes := make([]*raft.Raft, n)
	peerAddrs := map[int]string{
		0: "localhost:5000",
		1: "localhost:5001",
		2: "localhost:5002",
		3: "localhost:5003",
		4: "localhost:5004",
	}

	for i := 0; i < n; i++ {
		nodes[i] = raft.MakeRaft(i, peersIds, peerAddrs[i], peerAddrs)
	}

	fmt.Println("Start Simulation...")
	time.Sleep(1 * time.Second)

	// 5. Monitoring Loop
	go func() {
		for {
			fmt.Println("\n--- Scoreboard ---")
			for i := range n {
				state, term := nodes[i].GetState()
				fmt.Printf("Node %d: %s (Term %d)\n", i, state, term)
			}
			time.Sleep(1 * time.Second) // add this
		}
	}()
	// fmt.Println("\n--- 📊 Final Scoreboard ---")
	// for i := 0; i < n; i++ {
	// 	state, term := nodes[i].GetState()
	// 	fmt.Printf("Node %d: %v (Term %d)\n", i, state, term)
	// }

	// Block Forever
	select {}
}
