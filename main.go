package main

import (
	"context"
	"fmt"
	pb "raft-visualiser/proto"
	"raft-visualiser/raft"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {

	// Initialise a cluster of 5 Raft nodes
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

	// RPC Client to submit commands to the cluster

	fmt.Println("Start Simulation...")
	time.Sleep(1 * time.Second)

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

	go func() {
		time.Sleep(5 * time.Second) // wait for leader to stabilize

		conn, err := grpc.NewClient(peerAddrs[0], grpc.WithTransportCredentials(insecure.NewCredentials()))

		if err != nil {
			fmt.Printf("Failed to connect to node 0: %v\n", err)
			return
		}
		client := pb.NewRaftClient(conn)

		for i := 0; ; i++ {
			cmd := []byte(fmt.Sprintf("cmd-%d", i))
			reply, err := client.SubmitCommand(context.Background(), &pb.SubmitCommandArgs{Command: cmd})

			if err != nil {
				fmt.Printf("Failed to submit command: %v\n", err)
				return
			}

			if !reply.Success {
				conn.Close()

				// Redirect to leader
				addr := fmt.Sprintf("localhost:%d", 5000+reply.LeaderId)

				conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					fmt.Println("failed to reconnect:", err)
					continue
				}

				client = pb.NewRaftClient(conn)
				// Retry the command
				i--
				time.Sleep(500 * time.Millisecond)
				continue
			}

			fmt.Printf("Command %d submitted to leader %d \n", i, reply.LeaderId)
			time.Sleep(2 * time.Second)
		}
	}()

	// Block Forever
	select {}
}
