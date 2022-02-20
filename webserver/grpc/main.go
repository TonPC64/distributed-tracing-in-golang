package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc"

	"github.com/TonPC64/distributed-tracing-in-golang/webserver/grpc/api"
)

func init() {
	var exporter sdktrace.SpanExporter
	var err error

	exporter, err = zipkin.New("http://zipkin:9411/api/v2/spans")
	if err != nil {
		panic(err)
	}

	exporter, err = jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint("http://jaeger:14268/api/traces")))
	if err != nil {
		panic(err)
	}

	resources := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("webserver-grpc"),
		semconv.ServiceVersionKey.String("1.0.0"),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resources),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(b3.New())
}

type server struct {
	api.HelloServiceServer
}

func (s *server) SayHello(ctx context.Context, in *api.HelloRequest) (*api.HelloResponse, error) {
	tracer := otel.GetTracerProvider().Tracer("webserver-grpc")
	_, span := tracer.Start(ctx, "SayHello-span")
	defer span.End()

	fmt.Println(ctx)
	log.Printf("Received: %v\n", in.GetGreeting())
	time.Sleep(50 * time.Millisecond)

	return &api.HelloResponse{Reply: "Hello " + in.Greeting}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":"+os.Getenv("PORT"))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()),
	)

	api.RegisterHelloServiceServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
