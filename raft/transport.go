package raft

import (
	"context"
	pb "raft-visualiser/proto"
)

type Transport interface {
	RequestVote(ctx context.Context, peer int, args *pb.RequestVoteArgs) (*pb.RequestVoteReply, error)
	AppendEntries(ctx context.Context, peer int, args *pb.AppendEntriesArgs) (*pb.AppendEntriesReply, error)
}

type grpcTransport struct {
	clients map[int]pb.RaftClient
}

func (t *grpcTransport) RequestVote(ctx context.Context, peer int, args *pb.RequestVoteArgs) (*pb.RequestVoteReply, error) {
	return t.clients[peer].RequestVote(ctx, args)
}

func (t *grpcTransport) AppendEntries(ctx context.Context, peer int, args *pb.AppendEntriesArgs) (*pb.AppendEntriesReply, error) {
	return t.clients[peer].AppendEntries(ctx, args)
}
