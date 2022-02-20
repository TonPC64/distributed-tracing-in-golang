package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func init() {
	var exporter sdktrace.SpanExporter
	var err error

	// use zipkin exporter
	exporter, err = zipkin.New("http://zipkin:9411/api/v2/spans")
	if err != nil {
		panic(err)
	}

	// use jaeger exporter
	exporter, err = jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint("http://jaeger:14268/api/traces")))
	if err != nil {
		panic(err)
	}

	// new resource with attributes
	resources := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("webserver-http"),
		semconv.ServiceVersionKey.String("1.0.0"),
	)

	// new tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resources),
	)

	// inject tracer provider
	otel.SetTracerProvider(provider)

	// inject propagators
	// use b3 default config (single header)
	otel.SetTextMapPropagator(b3.New())
}

func main() {
	// create server
	mux := http.NewServeMux()

	// wrapped handler for use propagation to extract trace signal from http header to request context
	// it like a middleware
	wrappedHandler := otelhttp.NewHandler(http.HandlerFunc(httpHandler), "http-server")
	mux.Handle("/", wrappedHandler)

	// start server
	port := os.Getenv("PORT")
	log.Debug().Msg("starting server at http://localhost:" + port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Panic().Err(err).Msg("server error")
	}
}

// general http handler
func httpHandler(w http.ResponseWriter, r *http.Request) {
	// get tracer provider from otel package and inject ctx to tracer
	tracer := otel.GetTracerProvider().Tracer("httpHandler")
	ctx, span := tracer.Start(r.Context(), "httpHandler")
	defer span.End()

	// create http client with transport for inject trace signal to http header automatically
	client := &http.Client{
		Transport: otelhttp.NewTransport(nil),
	}
	// create http request with context for inject trace signal to http header in request
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://echo:8081", nil)
	if err != nil {
		// if error should set status to span
		span.RecordError(err)
		span.SetStatus(codes.Error, codes.Error.String())

		// write response like general handler
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("new request error"))

		return
	}

	// use client to call other service
	res, err := client.Do(request)
	if err != nil {
		// if error should set status to span
		span.RecordError(err)
		span.SetStatus(codes.Error, codes.Error.String())

		// write response like general handler
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("http request error"))

		return
	}

	fmt.Println(res.Status)
	if res.StatusCode != http.StatusOK {
		// if error should set status to span
		span.RecordError(err)
		span.SetStatus(codes.Error, codes.Error.String())

		// write response like general handler
		bb, _ := io.ReadAll(res.Body)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(bb)

		return
	}

	w.Write([]byte("ok with tracing"))
}
