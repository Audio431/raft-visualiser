package main

import (
	"fmt"
	"raft-visualiser/network" // Import 2 อันนี้
	"raft-visualiser/raft"
	"time"
)

func main() {
	net := network.MakeNetwork()

	n := 5
	peersIds := []int{0, 1, 2, 3, 4}
	nodes := make([]*raft.Raft, n)

	for i := 0; i < n; i++ {
		nodes[i] = raft.MakeRaft(i, peersIds, net)
	}

	fmt.Println("Start Simulation...")
	time.Sleep(1 * time.Second)

	// 5. Monitoring Loop
	go func() {
		for {
			fmt.Println("\n--- 📊 Scoreboard ---")
			for i := 0; i < n; i++ {
				state, term := nodes[i].State, nodes[i].CurrentTerm
				fmt.Printf("Node %d: %s (Term %d)\n", i, state, term)
			}
			time.Sleep(2 * time.Second)
		}
	}()

	// Block Forever
	select {}
}
