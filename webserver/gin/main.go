package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/Shopify/sarama"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/github.com/Shopify/sarama/otelsarama"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

var producer sarama.AsyncProducer

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
		semconv.ServiceNameKey.String("webserver-gin"),
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

func init() {
	config := sarama.NewConfig()
	config.Version = sarama.V2_5_0_0
	// So we can know the partition and offset of messages.
	config.Producer.Return.Successes = true

	var err error
	producer, err = sarama.NewAsyncProducer([]string{"kafka:9092"}, config)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to start Sarama producer")
	}

	// Wrap instrumentation
	producer = otelsarama.WrapAsyncProducer(config, producer)

	// We will log to STDOUT if we're not able to produce messages.
	go func() {
		for err := range producer.Errors() {
			fmt.Println("Failed to write message:", err)
		}
	}()
}

func main() {
	r := gin.Default()
	r.Use(otelgin.Middleware("webserver-gin"))
	r.GET("/", handler)

	if err := r.Run(); err != nil {
		log.Panic().Err(err).Msg("gin server error")
	}
}

func handler(c *gin.Context) {
	tracer := otel.GetTracerProvider().Tracer("gin-hander")
	ctx, span := tracer.Start(c.Request.Context(), "handler-span")
	defer span.End()

	fmt.Println(c.Request.Header)

	rand.Seed(time.Now().Unix())
	msg := sarama.ProducerMessage{
		Topic: "example-topic",
		Key:   sarama.StringEncoder("random_number"),
		Value: sarama.StringEncoder(fmt.Sprintf("%d", rand.Intn(1000))),
	}
	otel.GetTextMapPropagator().Inject(ctx, otelsarama.NewProducerMessageCarrier(&msg))

	producer.Input() <- &msg
	successMsg := <-producer.Successes()
	fmt.Println("Successful to write message, offset:", successMsg.Offset)

	c.JSON(http.StatusOK, gin.H{
		"message": http.StatusText(http.StatusOK),
	})
}
