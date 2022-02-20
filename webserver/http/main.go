package main

import (
	"net/http"
	"os"

	"github.com/rs/zerolog/log"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func main() {
	exporter, err := zipkin.New("http://zipkin:9411/api/v2/spans")
	if err != nil {
		panic(err)
	}

	resources := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("webserver-http"),
		semconv.ServiceVersionKey.String("1.0.0"),
	)

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resources),
	)
	otel.SetTracerProvider(provider)

	mux := http.NewServeMux()
	handler := http.HandlerFunc(httpHandler)
	wrappedHandler := otelhttp.NewHandler(handler, "hello-instrumented")
	mux.Handle("/", wrappedHandler)

	port := os.Getenv("PORT")
	log.Debug().Msg("starting server at http://localhost:" + port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Panic().Err(err).Msg("server error")
	}
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
	tracer := otel.GetTracerProvider().Tracer("httpHandler")
	_, span := tracer.Start(r.Context(), "httpHandler")
	defer span.End()

	if _, err := w.Write([]byte("ok with tracing")); err != nil {
		log.Error().Err(err).Msg("write response failed")
	}
}
