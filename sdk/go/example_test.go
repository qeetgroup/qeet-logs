package qeetlogs_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	qeetlogs "github.com/qeetgroup/qeet-logs/sdk/go"
)

func Example_ingest() {
	client, err := qeetlogs.New(qeetlogs.Config{
		APIKey:  os.Getenv("QEET_LOGS_API_KEY"),
		BaseURL: "https://api.logs.qeet.in",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()

	// Builder style.
	if err := client.Log("payments-service").
		Level("error").
		Body("stripe webhook signature mismatch").
		Attr("request_id", "req_xyz").
		Attr("user_id", "usr_abc").
		NewTrace().
		Send(ctx); err != nil {
		log.Fatal(err)
	}

	// Batch style.
	if err := client.IngestBatch(ctx, []qeetlogs.LogRecord{
		{Service: "api", Level: "info", Body: "request processed"},
		{Service: "api", Level: "warn", Body: "slow query detected", Attributes: map[string]any{"duration_ms": 850}},
	}); err != nil {
		log.Fatal(err)
	}

	fmt.Println("ingested")
	// Output: ingested
}

func Example_query() {
	client, err := qeetlogs.New(qeetlogs.Config{
		APIKey: os.Getenv("QEET_LOGS_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Query(context.Background(), qeetlogs.QueryParams{
		Q:     `service="payments-service" | level="error"`,
		From:  time.Now().Add(-1 * time.Hour),
		To:    time.Now(),
		Limit: 50,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range resp.Records {
		fmt.Println(r.Timestamp.Format(time.RFC3339), r.Service, r.Level, r.Body)
	}
}

func Example_tail() {
	client, err := qeetlogs.New(qeetlogs.Config{
		APIKey: os.Getenv("QEET_LOGS_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err = client.Tail(ctx, qeetlogs.TailParams{Service: "api"}, func(r qeetlogs.TailRecord) error {
		fmt.Printf("[%s] %s: %s\n", r.Level, r.Service, r.Body)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
