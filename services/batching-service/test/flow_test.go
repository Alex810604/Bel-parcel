package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/segmentio/kafka-go"
)

// TestStage2_EndToEndFlow implements "ЭТАП 2: ТЕСТ СКВОЗНОГО ПОТОКА"
// Scenario: Vitebsk -> Minsk Hub -> Gomel/Brest/Grodno
func TestStage2_EndToEndFlow(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("Skipping E2E test. Set E2E=1 to run.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Start Infrastructure (Postgres, Kafka)
	// We assume docker-compose is running or we start it here.
	// For simplicity, we assume it's running if E2E=1 is set, or we can try to start it.
	// In this environment, we can't easily start docker-compose if not already running,
	// but we can try to connect. If it fails, we fail.

	// Start batching-service
	// We need to make sure the DB is clean or at least we use unique IDs.
	// We'll clean up the DB tables we use.

	dbDSN := "postgres://user:pass@localhost:5432/trip_db?sslmode=disable"
	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	// Initialize Schema (Manual, because EnsureSchema might be incomplete for 'batches' table in code)
	initSchema(t, db)

	// Start batching-service process
	cmd := exec.CommandContext(ctx, "go", "run", "../cmd/batching-service/main.go")
	cmd.Env = append(os.Environ(),
		"DB_DSN="+dbDSN,
		"SERVER_PORT=0",
		"KAFKA_BROKERS=127.0.0.1:9092",
		"KAFKA_PRODUCE_TOPIC=batches.formed",
		"KAFKA_CONSUMETOPICS=orders.created,events.batch_delivered_to_pvp,events.reference_updated",
		"KAFKA_GROUP_ID=batching-stage2-test-"+uuid.NewString(),
		"BATCHING_MAX_SIZE=10", // Set to 10 to match our test case exactly or flush immediately
		"BATCHING_FLUSH_INTERVAL=5s",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start batching-service: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	time.Sleep(5 * time.Second) // Wait for service startup

	// 2. Setup Reference Data
	// Warehouse: Vitebsk
	// Hub: Minsk
	// Destinations: Gomel, Brest, Grodno
	runID := uuid.NewString()[:8]
	warehouseID := "wh-vitebsk-" + runID
	hubID := "pvp-minsk-hub-" + runID
	destGomel := "pvp-gomel-" + runID
	destBrest := "pvp-brest-" + runID
	destGrodno := "pvp-grodno-" + runID

	seedReferenceData(t, db, warehouseID, hubID, destGomel, destBrest, destGrodno)

	// 3. Create Kafka Writer
	// Ensure topics exist
	conn, err := kafka.Dial("tcp", "127.0.0.1:9092")
	if err != nil {
		t.Fatalf("failed to dial kafka: %v", err)
	}

	topicConfigs := []kafka.TopicConfig{
		{Topic: "orders.created", NumPartitions: 1, ReplicationFactor: 1},
		{Topic: "batches.formed", NumPartitions: 1, ReplicationFactor: 1},
		{Topic: "events.batch_delivered_to_pvp", NumPartitions: 1, ReplicationFactor: 1},
		{Topic: "events.reference_updated", NumPartitions: 1, ReplicationFactor: 1},
	}
	err = conn.CreateTopics(topicConfigs...)
	if err != nil {
		t.Logf("CreateTopics warning (might exist): %v", err)
	}
	conn.Close()

	writer := &kafka.Writer{
		Addr:     kafka.TCP("127.0.0.1:9092"),
		Balancer: &kafka.LeastBytes{},
	}
	defer writer.Close()

	// 4. Send 10 Orders
	// 4 -> Gomel
	// 3 -> Brest
	// 3 -> Grodno
	// All from Vitebsk Warehouse
	orders := []struct {
		ID     string
		DestID string
	}{
		{"ord-1-" + runID, destGomel}, {"ord-2-" + runID, destGomel}, {"ord-3-" + runID, destGomel}, {"ord-4-" + runID, destGomel},
		{"ord-5-" + runID, destBrest}, {"ord-6-" + runID, destBrest}, {"ord-7-" + runID, destBrest},
		{"ord-8-" + runID, destGrodno}, {"ord-9-" + runID, destGrodno}, {"ord-10-" + runID, destGrodno},
	}

	messages := make([]kafka.Message, 0, len(orders))
	for _, o := range orders {
		evtID := uuid.NewString()
		payload := map[string]interface{}{
			"event_id":       evtID,
			"event_type":     "orders.created",
			"occurred_at":    time.Now(),
			"correlation_id": o.ID,
			"data": map[string]interface{}{
				"order_id":            o.ID,
				"seller_warehouse_id": warehouseID,
				"pickup_point_id":     o.DestID,
				"created_at":          time.Now(),
				"customer_phone":      "+375291111111",
				"customer_email":      "test@example.com",
			},
		}
		b, _ := json.Marshal(payload)
		messages = append(messages, kafka.Message{
			Topic: "orders.created",
			Key:   []byte(warehouseID), // Group by warehouse
			Value: b,
		})
	}

	if err := writer.WriteMessages(ctx, messages...); err != nil {
		t.Fatalf("failed to send orders: %v", err)
	}
	t.Log("Sent 10 orders")

	// 5. Wait for Batch Formation (Vitebsk -> Minsk Hub)
	// Since maxSize=10, it should trigger immediately after all 10 are processed.
	// We listen to 'batches.formed'
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"127.0.0.1:9092"},
		GroupID: "test-verifier-stage2-" + uuid.NewString(),
		Topic:   "batches.formed",
		MaxWait: 1 * time.Second,
	})
	defer reader.Close()

	var firstBatchID string
	var firstBatchDestID string

	// Loop to find the valid batch for this run (skipping old junk)
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			t.Fatalf("failed to fetch first batch: %v", err)
		}

		var envelope struct {
			Data struct {
				BatchID          string   `json:"batch_id"`
				DestinationID    string   `json:"destination_id"`
				IsHubDestination bool     `json:"is_hub_destination"`
				OrderIDs         []string `json:"order_ids"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg.Value, &envelope); err != nil {
			t.Logf("failed to unmarshal batch event (skipping): %v", err)
			continue
		}

		// Check if batch exists in current DB
		var exists bool
		err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM batches WHERE id=$1)", envelope.Data.BatchID).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check batch existence: %v", err)
		}
		if !exists {
			t.Logf("Skipping old/orphaned batch ID: %s", envelope.Data.BatchID)
			_ = reader.CommitMessages(ctx, msg)
			continue
		}

		t.Logf("Received Batch 1: ID=%s, Dest=%s, IsHub=%v, Orders=%d",
			envelope.Data.BatchID, envelope.Data.DestinationID, envelope.Data.IsHubDestination, len(envelope.Data.OrderIDs))

		if len(envelope.Data.OrderIDs) != 10 {
			t.Fatalf("Expected 10 orders in first batch, got %d", len(envelope.Data.OrderIDs))
		}
		if !envelope.Data.IsHubDestination {
			t.Fatalf("Expected is_hub_destination=true")
		}
		if envelope.Data.DestinationID != hubID {
			t.Fatalf("Expected destination=%s, got %s", hubID, envelope.Data.DestinationID)
		}

		firstBatchID = envelope.Data.BatchID
		firstBatchDestID = envelope.Data.DestinationID
		_ = reader.CommitMessages(ctx, msg)
		break
	}

	// 6. Simulate Delivery to Hub (Algorithm 7 Trigger)
	// Send "events.batch_delivered_to_pvp"
	deliveryPayload := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "events.batch_delivered_to_pvp",
		"occurred_at":    time.Now(),
		"correlation_id": firstBatchID,
		"data": map[string]interface{}{
			"batch_id":     firstBatchID,
			"is_hub":       true,
			"pvp_id":       firstBatchDestID, // Minsk Hub
			"delivered_at": time.Now(),
		},
	}
	b, _ := json.Marshal(deliveryPayload)

	t.Logf("Sending delivery event for batch %s to topic events.batch_delivered_to_pvp", firstBatchID)
	if err := writer.WriteMessages(ctx, kafka.Message{
		Topic: "events.batch_delivered_to_pvp",
		Key:   []byte(firstBatchID),
		Value: b,
	}); err != nil {
		t.Fatalf("failed to send delivery event: %v", err)
	}
	t.Log("Sent delivery to hub event")

	// VERIFY: Read back the message to ensure it's in Kafka
	verifyReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{"127.0.0.1:9092"},
		Topic:     "events.batch_delivered_to_pvp",
		Partition: 0,
		MaxWait:   5 * time.Second,
	})
	// Just try to read to ensure it's there
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer verifyCancel()
	if _, err := verifyReader.FetchMessage(verifyCtx); err != nil {
		t.Logf("WARNING: Could not verify message in events.batch_delivered_to_pvp: %v", err)
	} else {
		t.Log("VERIFIED: Message found in events.batch_delivered_to_pvp")
	}
	verifyReader.Close()

	// 7. Verify 3 New Batches Formed
	// We expect 3 messages in 'batches.formed'
	// Destinations: Gomel, Brest, Grodno
	expectedDests := map[string]int{
		destGomel:  4,
		destBrest:  3,
		destGrodno: 3,
	}
	receivedDests := make(map[string]int)
	seenBatches := make(map[string]struct{})

	// Helper to fetch next unique batch
	fetchNextBatch := func() (string, string, string, []string) {
		for {
			msg, err := reader.FetchMessage(ctx)
			if err != nil {
				t.Fatalf("failed to fetch message: %v", err)
			}
			var env struct {
				Data struct {
					BatchID       string   `json:"batch_id"`
					DestinationID string   `json:"destination_id"`
					OriginID      string   `json:"origin_id"`
					OrderIDs      []string `json:"order_ids"`
				} `json:"data"`
			}
			if err := json.Unmarshal(msg.Value, &env); err != nil {
				t.Logf("failed to unmarshal (skipping): %v", err)
				_ = reader.CommitMessages(ctx, msg)
				continue
			}

			if _, dup := seenBatches[env.Data.BatchID]; dup {
				_ = reader.CommitMessages(ctx, msg)
				continue
			}

			// Check if batch exists in current DB (to filter out old Kafka messages)
			var exists bool
			err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM batches WHERE id=$1)", env.Data.BatchID).Scan(&exists)
			if err != nil {
				t.Fatalf("failed to check batch existence: %v", err)
			}
			if !exists {
				t.Logf("Skipping old/orphaned batch ID: %s", env.Data.BatchID)
				_ = reader.CommitMessages(ctx, msg)
				continue
			}

			if env.Data.BatchID == firstBatchID {
				t.Logf("Skipping duplicate/original batch ID: %s", env.Data.BatchID)
				_ = reader.CommitMessages(ctx, msg)
				continue
			}
			_ = reader.CommitMessages(ctx, msg)
			seenBatches[env.Data.BatchID] = struct{}{}
			return env.Data.BatchID, env.Data.OriginID, env.Data.DestinationID, env.Data.OrderIDs
		}
	}

	for len(receivedDests) < 3 {
		bid, origin, dest, oids := fetchNextBatch()
		t.Logf("Received Disband Batch: ID=%s, Origin=%s, Dest=%s, Orders=%d", bid, origin, dest, len(oids))
		if origin != hubID {
			t.Errorf("Expected origin=%s, got %s", hubID, origin)
		}
		if _, ok := receivedDests[dest]; ok {
			continue
		}
		receivedDests[dest] = len(oids)
	}

	// Verify counts
	for dest, count := range expectedDests {
		if got, ok := receivedDests[dest]; !ok {
			t.Errorf("Missing batch for destination %s", dest)
		} else if got != count {
			t.Errorf("Wrong order count for %s: expected %d, got %d", dest, count, got)
		}
	}

	t.Log("Test Stage 2 Passed Successfully")
}

func initSchema(t *testing.T, db *sql.DB) {
	// Create tables if not exist (including 'batches' and 'batch_orders')
	sqls := []string{
		`CREATE TABLE IF NOT EXISTS batches (
			id TEXT PRIMARY KEY,
			origin_id TEXT,
			origin_type TEXT DEFAULT 'warehouse',
			seller_warehouse_id TEXT,
			pickup_point_id TEXT,
			origin_lat DOUBLE PRECISION,
			origin_lng DOUBLE PRECISION,
			destination_lat DOUBLE PRECISION,
			destination_lng DOUBLE PRECISION,
			is_hub_destination BOOLEAN,
			formed_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS batch_orders (
			batch_id TEXT NOT NULL,
			order_id TEXT NOT NULL,
			destination_pvp_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS ref_warehouses (
			warehouse_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			latitude DOUBLE PRECISION NOT NULL,
			longitude DOUBLE PRECISION NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS ref_pickup_points (
			pvp_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			latitude DOUBLE PRECISION NOT NULL,
			longitude DOUBLE PRECISION NOT NULL,
			is_hub BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS processed_events (
			event_id TEXT PRIMARY KEY,
			occurred_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS batch_group_items (
			warehouse_id TEXT NOT NULL,
			pvp_id TEXT NOT NULL,
			order_id TEXT NOT NULL,
			customer_phone TEXT,
			customer_email TEXT,
			updated_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (warehouse_id, pvp_id, order_id)
		)`,
		// Clean up for test
		`TRUNCATE TABLE batch_group_items, processed_events, batch_orders, batches, ref_warehouses, ref_pickup_points`,
	}

	for _, s := range sqls {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("failed to init schema: %v\nSQL: %s", err, s)
		}
	}
	// Try to truncate outbox_events if it exists
	_, _ = db.Exec("TRUNCATE TABLE outbox_events")
}

func seedReferenceData(t *testing.T, db *sql.DB, whID, hubID, dest1, dest2, dest3 string) {
	// Warehouses
	_, err := db.Exec(`
		INSERT INTO ref_warehouses (warehouse_id, name, latitude, longitude)
		VALUES ($1, 'Vitebsk Main', 55.1848, 30.2016)
		ON CONFLICT (warehouse_id) DO NOTHING
	`, whID)
	if err != nil {
		t.Fatalf("failed to seed warehouse: %v", err)
	}

	// PVPs
	pvps := []struct {
		ID    string
		Name  string
		Lat   float64
		Lng   float64
		IsHub bool
	}{
		{hubID, "Minsk Hub", 53.9006, 27.5590, true},
		{dest1, "Gomel PVP", 52.4345, 30.9754, false},
		{dest2, "Brest PVP", 52.0976, 23.7341, false},
		{dest3, "Grodno PVP", 53.6884, 23.8258, false},
	}

	for _, p := range pvps {
		_, err := db.Exec(`
			INSERT INTO ref_pickup_points (pvp_id, name, latitude, longitude, is_hub)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (pvp_id) DO NOTHING
		`, p.ID, p.Name, p.Lat, p.Lng, p.IsHub)
		if err != nil {
			t.Fatalf("failed to seed pvp %s: %v", p.ID, err)
		}
	}
}
