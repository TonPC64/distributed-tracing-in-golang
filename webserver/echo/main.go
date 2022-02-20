package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gojek/heimdall/v7/httpclient"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

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
		semconv.ServiceNameKey.String("webserver-echo"),
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

var grpcClient api.HelloServiceClient

func main() {
	var conn *grpc.ClientConn
	conn, err := grpc.Dial("host.docker.internal:6565", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()),
	)

	if err != nil {
		log.Fatalf("did not connect: %s", err)
	}
	defer func() { _ = conn.Close() }()

	grpcClient = api.NewHelloServiceClient(conn)

	e := echo.New()
	e.Use(otelecho.Middleware("webserver-echo"))

	e.GET("/", handler)

	e.Logger.Fatal(e.Start(":" + os.Getenv("PORT")))
}

func handler(c echo.Context) error {
	tracer := otel.GetTracerProvider().Tracer("echo-hander")
	ctx, span := tracer.Start(c.Request().Context(), "handler-span")
	defer span.End()

	fmt.Println(c.Request().Header)
	client := httpclient.NewClient(httpclient.WithHTTPClient(&http.Client{
		Transport: otelhttp.NewTransport(nil),
	}))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://gin:8082", nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, codes.Error.String())

		fmt.Println(err)

		return err
	}

	res, err := client.Do(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, codes.Error.String())

		fmt.Println(err)

		return err
	}

	fmt.Println(res.Status)

	callSayHello(ctx, grpcClient)

	return c.String(http.StatusOK, "Hello, World!")
}

func callSayHello(ctx context.Context, c api.HelloServiceClient) {
	md := metadata.Pairs(
		"timestamp", time.Now().Format(time.StampNano),
	)

	ctx = metadata.NewOutgoingContext(ctx, md)
	response, err := c.SayHello(ctx, &api.HelloRequest{Greeting: "World"})
	if err != nil {
		log.Fatalf("Error when calling SayHello: %s", err)
	}
	log.Printf("Response from server: %s", response.Reply)
}
