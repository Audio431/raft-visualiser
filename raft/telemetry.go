package raft

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const otlpEndpoint = "localhost:4317" // OTEL Collector, OTLP/gRPC

type telemetry struct {
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
	conn   *grpc.ClientConn // not owned by the exporter; we close it
}

func newTelemetry(nodeID int) (*telemetry, error) {
	conn, err := grpc.NewClient(
		otlpEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("node %d: OTLP gRPC client: %w", nodeID, err)
	}

	exporter, err := otlptracegrpc.New(context.Background(), otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("node %d: OTLP exporter: %w", nodeID, err)
	}

	res := resource.NewWithAttributes(
		"https://opentelemetry.io/schemas/1.26.0",
		attribute.String("service.name", fmt.Sprintf("raft-node-%d", nodeID)),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	return &telemetry{
		tp:     tp,
		tracer: tp.Tracer("raft-visualiser/raft"),
		conn:   conn,
	}, nil
}

// Shutdown flushes buffered spans, stops the exporter, then closes the gRPC connection.
func (t *telemetry) Shutdown(ctx context.Context) error {
	err := t.tp.Shutdown(ctx) // flushes batcher + stops exporter
	t.conn.Close()            // conn is not closed by tp.Shutdown
	return err
}
