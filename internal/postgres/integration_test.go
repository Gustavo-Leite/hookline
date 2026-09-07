package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Gustavo-Leite/hookline/internal/apikey"
	"github.com/Gustavo-Leite/hookline/internal/delivery"
	"github.com/Gustavo-Leite/hookline/internal/endpoint"
	"github.com/Gustavo-Leite/hookline/internal/event"
	"github.com/Gustavo-Leite/hookline/internal/secrets"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	code, err := runWithPostgres(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration tests:", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func runWithPostgres(m *testing.M) (int, error) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("hookline"),
		tcpostgres.WithUsername("hookline"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return 0, fmt.Errorf("starting postgres: %w", err)
	}
	defer func() { _ = container.Terminate(ctx) }()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 0, fmt.Errorf("reading connection string: %w", err)
	}

	if err := migrate(dsn); err != nil {
		return 0, err
	}

	testPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("opening pool: %w", err)
	}
	defer testPool.Close()

	return m.Run(), nil
}

func migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting dialect: %w", err)
	}

	goose.SetLogger(goose.NopLogger())

	if err := goose.Up(db, "../../migrations"); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

func newCipher(t *testing.T) *secrets.Cipher {
	t.Helper()

	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	c, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	return c
}

func newApplication(t *testing.T) uuid.UUID {
	t.Helper()

	requireContainer(t)

	id, err := NewApplicationStore(testPool).Create(t.Context(), t.Name())
	if err != nil {
		t.Fatalf("creating application: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM applications WHERE id = $1`, id)
	})

	return id
}

func requireContainer(t *testing.T) {
	t.Helper()

	if testPool == nil {
		t.Skip("integration test: needs Docker, skipped with -short")
	}
}

func newEndpoint(t *testing.T, store *EndpointStore, applicationID uuid.UUID, eventTypes []string) endpoint.Endpoint {
	t.Helper()

	secret, err := endpoint.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}

	created, err := store.Create(t.Context(), endpoint.Endpoint{
		ApplicationID: applicationID,
		URL:           "https://example.com/hooks",
		EventTypes:    eventTypes,
		Secret:        secret,
	})
	if err != nil {
		t.Fatalf("creating endpoint: %v", err)
	}

	return created
}

func TestEndpointSecretsAreEncryptedAtRest(t *testing.T) {
	applicationID := newApplication(t)
	store := NewEndpointStore(testPool, newCipher(t))

	created := newEndpoint(t, store, applicationID, []string{})

	var stored string
	err := testPool.QueryRow(t.Context(), `SELECT secret FROM endpoints WHERE id = $1`, created.ID).Scan(&stored)
	if err != nil {
		t.Fatalf("reading the raw column: %v", err)
	}

	if stored == created.Secret {
		t.Fatal("the secret was written to the database in clear text")
	}

	if !secrets.IsEncrypted(stored) {
		t.Errorf("stored value = %q, want it to carry the encryption marker", stored)
	}

	read, err := store.Get(t.Context(), applicationID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if read.Secret != created.Secret {
		t.Error("the secret did not survive the round trip through the database")
	}
}

func TestEndpointsAreIsolatedBetweenApplications(t *testing.T) {
	var (
		cipher = newCipher(t)
		store  = NewEndpointStore(testPool, cipher)
		mine   = newApplication(t)
		theirs = newApplication(t)
	)

	target := newEndpoint(t, store, theirs, []string{})

	t.Run("get", func(t *testing.T) {
		if _, err := store.Get(t.Context(), mine, target.ID); !errors.Is(err, endpoint.ErrNotFound) {
			t.Errorf("error = %v, want %v", err, endpoint.ErrNotFound)
		}
	})

	t.Run("update", func(t *testing.T) {
		disabled := true
		_, err := store.Update(t.Context(), mine, target.ID, endpoint.UpdateParams{Disabled: &disabled})

		if !errors.Is(err, endpoint.ErrNotFound) {
			t.Errorf("error = %v, want %v", err, endpoint.ErrNotFound)
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := store.Delete(t.Context(), mine, target.ID); !errors.Is(err, endpoint.ErrNotFound) {
			t.Errorf("error = %v, want %v", err, endpoint.ErrNotFound)
		}
	})

	t.Run("list", func(t *testing.T) {
		found, err := store.List(t.Context(), mine)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		if len(found) != 0 {
			t.Errorf("listed %d endpoints belonging to another application", len(found))
		}
	})
}

func publish(t *testing.T, store *EventStore, applicationID uuid.UUID, eventType, payload string, idempotencyKey *string) (event.Event, bool) {
	t.Helper()

	created, replayed, err := store.Create(t.Context(), event.Event{
		ApplicationID:  applicationID,
		Type:           eventType,
		Payload:        json.RawMessage(payload),
		IdempotencyKey: idempotencyKey,
		PayloadHash:    event.HashPayload(eventType, []byte(payload)),
	})
	if err != nil {
		t.Fatalf("publishing event: %v", err)
	}

	return created, replayed
}

func TestFanOutReachesOnlySubscribedEndpoints(t *testing.T) {
	var (
		applicationID = newApplication(t)
		endpoints     = NewEndpointStore(testPool, newCipher(t))
		events        = NewEventStore(testPool)
	)

	subscribed := newEndpoint(t, endpoints, applicationID, []string{"user.created"})
	catchAll := newEndpoint(t, endpoints, applicationID, []string{})
	other := newEndpoint(t, endpoints, applicationID, []string{"order.paid"})

	disabled := newEndpoint(t, endpoints, applicationID, []string{"user.created"})
	disable := true
	if _, err := endpoints.Update(t.Context(), applicationID, disabled.ID, endpoint.UpdateParams{Disabled: &disable}); err != nil {
		t.Fatalf("disabling endpoint: %v", err)
	}

	published, _ := publish(t, events, applicationID, "user.created", `{"id":1}`, nil)

	rows, err := testPool.Query(t.Context(), `SELECT endpoint_id FROM deliveries WHERE event_id = $1`, published.ID)
	if err != nil {
		t.Fatalf("reading deliveries: %v", err)
	}
	defer rows.Close()

	reached := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scanning delivery: %v", err)
		}
		reached[id] = true
	}

	for name, expected := range map[string]struct {
		id   uuid.UUID
		want bool
	}{
		"subscribed to this type": {subscribed.ID, true},
		"subscribed to all types": {catchAll.ID, true},
		"subscribed to another":   {other.ID, false},
		"disabled":                {disabled.ID, false},
	} {
		if reached[expected.id] != expected.want {
			t.Errorf("%s: delivered = %v, want %v", name, reached[expected.id], expected.want)
		}
	}
}

func TestIdempotentIngestion(t *testing.T) {
	var (
		applicationID = newApplication(t)
		endpoints     = NewEndpointStore(testPool, newCipher(t))
		events        = NewEventStore(testPool)
		key           = "same-key"
	)

	newEndpoint(t, endpoints, applicationID, []string{})

	first, replayed := publish(t, events, applicationID, "user.created", `{"id":1}`, &key)
	if replayed {
		t.Fatal("the first publish was reported as a replay")
	}

	second, replayed := publish(t, events, applicationID, "user.created", `{"id":1}`, &key)
	if !replayed {
		t.Error("the second publish was not reported as a replay")
	}

	if second.ID != first.ID {
		t.Errorf("replay returned event %s, want the original %s", second.ID, first.ID)
	}

	var deliveries int
	err := testPool.QueryRow(t.Context(),
		`SELECT count(*) FROM deliveries WHERE application_id = $1`, applicationID).Scan(&deliveries)
	if err != nil {
		t.Fatalf("counting deliveries: %v", err)
	}

	if deliveries != 1 {
		t.Errorf("the replay produced %d deliveries, want 1", deliveries)
	}

	_, _, err = events.Create(t.Context(), event.Event{
		ApplicationID:  applicationID,
		Type:           "user.created",
		Payload:        json.RawMessage(`{"id":999}`),
		IdempotencyKey: &key,
		PayloadHash:    event.HashPayload("user.created", []byte(`{"id":999}`)),
	})

	if !errors.Is(err, event.ErrIdempotencyKeyReused) {
		t.Errorf("reusing the key with another payload: error = %v, want %v", err, event.ErrIdempotencyKeyReused)
	}
}

func TestClaimHandsEachDeliveryToOneWorker(t *testing.T) {
	var (
		applicationID = newApplication(t)
		cipher        = newCipher(t)
		endpoints     = NewEndpointStore(testPool, cipher)
		events        = NewEventStore(testPool)
		deliveries    = NewDeliveryStore(testPool, cipher)
	)

	newEndpoint(t, endpoints, applicationID, []string{})

	const published = 6
	for range published {
		publish(t, events, applicationID, "user.created", `{"id":1}`, nil)
	}

	type result struct {
		jobs []delivery.Job
		err  error
	}

	results := make(chan result, 2)
	for range 2 {
		go func() {
			jobs, err := deliveries.Claim(context.Background(), published, time.Minute)
			results <- result{jobs: jobs, err: err}
		}()
	}

	claimed := map[uuid.UUID]int{}
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("Claim: %v", r.err)
		}

		for _, job := range r.jobs {
			claimed[job.DeliveryID]++
		}
	}

	if len(claimed) != published {
		t.Errorf("claimed %d distinct deliveries, want %d", len(claimed), published)
	}

	for id, times := range claimed {
		if times != 1 {
			t.Errorf("delivery %s was handed out %d times, want exactly once", id, times)
		}
	}
}

func TestClaimDecryptsTheEndpointSecret(t *testing.T) {
	var (
		applicationID = newApplication(t)
		cipher        = newCipher(t)
		endpoints     = NewEndpointStore(testPool, cipher)
		events        = NewEventStore(testPool)
		deliveries    = NewDeliveryStore(testPool, cipher)
	)

	created := newEndpoint(t, endpoints, applicationID, []string{})
	publish(t, events, applicationID, "user.created", `{"id":1}`, nil)

	jobs, err := deliveries.Claim(t.Context(), 10, time.Minute)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	var job delivery.Job
	for _, candidate := range jobs {
		if candidate.EventID != uuid.Nil() {
			job = candidate
		}
	}

	if job.Secret != created.Secret {
		t.Errorf("the worker received %q, want the endpoint secret in clear text", job.Secret)
	}
}

func TestRecordAttemptWritesHistoryAndAdvancesTheDelivery(t *testing.T) {
	var (
		applicationID = newApplication(t)
		cipher        = newCipher(t)
		endpoints     = NewEndpointStore(testPool, cipher)
		events        = NewEventStore(testPool)
		store         = NewDeliveryStore(testPool, cipher)
	)

	newEndpoint(t, endpoints, applicationID, []string{})
	publish(t, events, applicationID, "user.created", `{"id":1}`, nil)

	jobs, err := store.Claim(t.Context(), 1, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("Claim: %v (%d jobs)", err, len(jobs))
	}

	code := 500
	err = store.RecordAttempt(t.Context(), delivery.AttemptOutcome{
		DeliveryID:    jobs[0].DeliveryID,
		AttemptNumber: 1,
		Result:        delivery.Result{StatusCode: &code, Duration: 42 * time.Millisecond},
		Status:        delivery.StatusPending,
		NextAttemptAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	attempts, err := store.Attempts(t.Context(), applicationID, jobs[0].DeliveryID)
	if err != nil {
		t.Fatalf("Attempts: %v", err)
	}

	if len(attempts) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(attempts))
	}

	if attempts[0].StatusCode == nil || *attempts[0].StatusCode != code {
		t.Errorf("StatusCode = %v, want %d", attempts[0].StatusCode, code)
	}

	if attempts[0].Duration != 42*time.Millisecond {
		t.Errorf("Duration = %s, want 42ms", attempts[0].Duration)
	}

	found, err := store.Get(t.Context(), applicationID, jobs[0].DeliveryID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if found.Status != delivery.StatusPending || found.CompletedAt != nil {
		t.Errorf("delivery = %+v, want it still pending and not completed", found)
	}
}

func TestDeliveriesAreIsolatedBetweenApplications(t *testing.T) {
	var (
		cipher    = newCipher(t)
		endpoints = NewEndpointStore(testPool, cipher)
		events    = NewEventStore(testPool)
		store     = NewDeliveryStore(testPool, cipher)
		mine      = newApplication(t)
		theirs    = newApplication(t)
	)

	newEndpoint(t, endpoints, theirs, []string{})
	published, _ := publish(t, events, theirs, "user.created", `{"id":1}`, nil)

	var deliveryID uuid.UUID
	err := testPool.QueryRow(t.Context(), `SELECT id FROM deliveries WHERE event_id = $1`, published.ID).Scan(&deliveryID)
	if err != nil {
		t.Fatalf("reading their delivery: %v", err)
	}

	if _, err := store.Get(t.Context(), mine, deliveryID); !errors.Is(err, delivery.ErrNotFound) {
		t.Errorf("Get: error = %v, want %v", err, delivery.ErrNotFound)
	}

	if _, err := store.Replay(t.Context(), mine, deliveryID); !errors.Is(err, delivery.ErrNotFound) {
		t.Errorf("Replay: error = %v, want %v", err, delivery.ErrNotFound)
	}

	attempts, err := store.Attempts(t.Context(), mine, deliveryID)
	if err != nil {
		t.Fatalf("Attempts: %v", err)
	}

	if len(attempts) != 0 {
		t.Errorf("read %d attempts belonging to another application", len(attempts))
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	applicationID := newApplication(t)
	store := NewAPIKeyStore(testPool)

	key, err := apikey.Generate(apikey.EnvTest)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	created, err := store.Create(t.Context(), applicationID, "default", key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := store.FindByHash(t.Context(), apikey.Hash(key.Plaintext))
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("FindByHash returned %s, want %s", found.ID, created.ID)
	}

	if err := found.Validate(time.Now()); err != nil {
		t.Errorf("a fresh key was not usable: %v", err)
	}

	if _, err := store.FindByHash(t.Context(), apikey.Hash("hl_test_wrong")); !errors.Is(err, apikey.ErrNotFound) {
		t.Errorf("an unknown key: error = %v, want %v", err, apikey.ErrNotFound)
	}

	revoked, err := store.Revoke(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := revoked.Validate(time.Now()); !errors.Is(err, apikey.ErrRevoked) {
		t.Errorf("after revoking: error = %v, want %v", err, apikey.ErrRevoked)
	}

	if err := store.TouchLastUsed(t.Context(), created.ID); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	touched, err := store.ListByApplication(t.Context(), applicationID)
	if err != nil {
		t.Fatalf("ListByApplication: %v", err)
	}

	if len(touched) != 1 || touched[0].LastUsedAt == nil {
		t.Error("TouchLastUsed did not record the usage")
	}
}
