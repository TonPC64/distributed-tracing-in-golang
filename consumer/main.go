package main

import (
	"context"
	"log"

	"github.com/Shopify/sarama"

	"go.opentelemetry.io/contrib/instrumentation/github.com/Shopify/sarama/otelsarama"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
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
		semconv.ServiceNameKey.String("consumer-sarama"),
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

func main() {
	startConsumerGroup([]string{"kafka:9092"})

	select {}
}

func startConsumerGroup(brokerList []string) {
	consumerGroupHandler := Consumer{}
	// Wrap instrumentation
	handler := otelsarama.WrapConsumerGroupHandler(&consumerGroupHandler)

	config := sarama.NewConfig()
	config.Version = sarama.V2_5_0_0
	config.Consumer.Offsets.Initial = sarama.OffsetOldest

	// Create consumer group
	consumerGroup, err := sarama.NewConsumerGroup(brokerList, "example", config)
	if err != nil {
		log.Fatalln("Failed to start sarama consumer group:", err)
	}

	err = consumerGroup.Consume(context.Background(), []string{"example-topic"}, handler)
	if err != nil {
		log.Fatalln("Failed to consume via handler:", err)
	}
}

func printMessage(msg *sarama.ConsumerMessage) {
	// Extract tracing info from message
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), otelsarama.NewConsumerMessageCarrier(msg))

	tr := otel.GetTracerProvider().Tracer("consumer")
	_, span := tr.Start(ctx, "consume message", trace.WithAttributes(
		semconv.MessagingOperationProcess,
	))
	defer span.End()

	log.Println("Successful to read message: ", string(msg.Value))
}

// Consumer represents a Sarama consumer group consumer
type Consumer struct {
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (consumer *Consumer) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (consumer *Consumer) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages().
func (consumer *Consumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// NOTE:
	// Do not move the code below to a goroutine.
	// The `ConsumeClaim` itself is called within a goroutine, see:
	// https://github.com/Shopify/sarama/blob/master/consumer_group.go#L27-L29
	for message := range claim.Messages() {
		printMessage(message)
		session.MarkMessage(message, "")
	}

	return nil
}
