# Campaign Events API

A Go-based backend service for ingesting campaign events from third-party messaging providers and providing campaign-level analytics.

The service receives events such as `sent`, `delivered`, `opened`, and `clicked`, stores them in SQLite, prevents duplicate event ingestion, and exposes APIs to retrieve campaign statistics.

## 1. Tech Stack

- **Language:** Go
- **Database:** SQLite
- **HTTP Server:** Go standard library (`net/http`)
- **Database Driver:** `modernc.org/sqlite`
- **Data Format:** JSON

## 2. Project Structure

```text
starter/
├── database/
│   └── sqlite.go          # SQLite connection and table initialization
├── models/
│   └── event.go           # Event model
├── seed/                  # Seed data provided with the assignment
├── go.mod
├── go.sum
├── main.go                # HTTP server, routes, handlers and business logic
├── types.go               # Shared types, if used
└── README.md
```

## 3. Prerequisites

Install:

- Go 1.22 or later
- Git

Verify Go installation:

```bash
go version
```

## 4. Setup and Installation

Clone the repository:

```bash
git clone <your-repository-url>
```

Navigate to the starter directory:

```bash
cd starter
```

Download dependencies:

```bash
go mod tidy
```

Run the application:

```bash
go run .
```

The server starts at:

```text
http://localhost:8080
```

The SQLite database is initialized automatically when the application starts.

## 5. API Endpoints

| Method | Endpoint | Description |
|---|---|---|
| POST | `/events` | Ingest a batch of campaign events |
| GET | `/campaigns/{campaignID}/stats` | Retrieve campaign statistics |

---

## 6. POST /events

Receives a JSON array of events from third-party providers.

**URL:**

```text
POST http://localhost:8080/events
```

**Request body:**

```json
[
  {
    "event_id": "evt_001",
    "campaign_id": "cmp_summer",
    "contact_id": "ct_001",
    "type": "sent",
    "timestamp": "2026-08-10T09:00:00Z"
  },
  {
    "event_id": "evt_002",
    "campaign_id": "cmp_summer",
    "contact_id": "ct_001",
    "type": "delivered",
    "timestamp": "2026-08-10T09:01:00Z"
  },
  {
    "event_id": "evt_003",
    "campaign_id": "cmp_summer",
    "contact_id": "ct_001",
    "type": "opened",
    "timestamp": "2026-08-10T09:05:00Z",
    "metadata": {
      "device": "mobile"
    }
  }
]
```

### Event fields

| Field | Type | Description |
|---|---|---|
| `event_id` | String | Unique identifier assigned by the provider |
| `campaign_id` | String | Campaign associated with the event |
| `contact_id` | String | Contact associated with the event |
| `type` | String | Event type: sent, delivered, opened or clicked |
| `timestamp` | RFC3339 string | Time when the event occurred at the provider |
| `metadata` | JSON object | Optional additional event information |

### Validation rules

Each event is validated independently.

- `event_id` is required.
- `campaign_id` is required.
- `contact_id` is required.
- `type` must be one of `sent`, `delivered`, `opened`, or `clicked`.
- `timestamp` must be a valid RFC3339 timestamp.
- Invalid events are rejected without preventing other valid events in the batch from being processed.

### Successful response

HTTP status: `200 OK`

```json
{
  "received": 3,
  "accepted": 3,
  "duplicates": 0,
  "rejected": 0,
  "errors": []
}
```

### Duplicate event handling

The service uses `event_id` as the primary key in SQLite.

If the same event is received again, it is not inserted a second time.

Example response when all three events have already been received:

```json
{
  "received": 3,
  "accepted": 0,
  "duplicates": 3,
  "rejected": 0,
  "errors": []
}
```

### Invalid event handling

Invalid events are reported individually.

Example:

```json
{
  "received": 2,
  "accepted": 1,
  "duplicates": 0,
  "rejected": 1,
  "errors": [
    {
      "index": 1,
      "event_id": "evt_002",
      "reason": "invalid event type"
    }
  ]
}
```

Malformed JSON request bodies are rejected with HTTP `400 Bad Request`.

---

## 7. GET /campaigns/{campaignID}/stats

Returns aggregated statistics for a specific campaign.

**URL:**

```text
GET http://localhost:8080/campaigns/cmp_summer/stats
```

### Successful response

HTTP status: `200 OK`

```json
{
  "campaign_id": "cmp_summer",
  "sent": 100,
  "delivered": 90,
  "opened": 45,
  "clicked": 12,
  "unique_opens": 40,
  "daily_delivered": [
    {
      "date": "2026-08-10",
      "count": 60
    },
    {
      "date": "2026-08-11",
      "count": 30
    }
  ]
}
```

### Statistics definitions

| Metric | Meaning |
|---|---|
| `sent` | Total unique sent events |
| `delivered` | Total unique delivered events |
| `opened` | Total unique opened event records |
| `clicked` | Total unique clicked events |
| `unique_opens` | Number of distinct contacts who opened the campaign |
| `daily_delivered` | Delivered event counts grouped by UTC date |

**Important:** `opened` and `unique_opens` represent different metrics.

For example, if one contact opens the same campaign three times:

- `opened = 3`
- `unique_opens = 1`

A campaign with no events returns zero counts and an empty `daily_delivered` array.

---

## 8. Database Design

The service uses a single SQLite table named `events`.

### Events table

| Column | Type | Description |
|---|---|---|
| `event_id` | TEXT PRIMARY KEY | Unique event identifier |
| `campaign_id` | TEXT | Campaign identifier |
| `contact_id` | TEXT | Contact identifier |
| `type` | TEXT | Event type |
| `timestamp` | TEXT | Provider event timestamp stored in UTC |
| `metadata` | TEXT | Optional JSON metadata |
| `received_at` | TEXT | Time the event was received by Relay |

### Indexes

- Primary key on `event_id` prevents duplicate event insertion.
- Index on `campaign_id` improves campaign-specific queries.
- A campaign and timestamp index can support time-based queries.

---

## 9. Important Implementation Decisions

### Idempotency

The provider may retry an event when it does not receive a timely successful response.

To handle retries, the service uses `event_id` as a primary key.

The SQL insertion uses:

```sql
INSERT INTO events (...)
VALUES (...)
ON CONFLICT(event_id) DO NOTHING;
```

This ensures that an existing event is not inserted again, including when concurrent requests contain the same event ID.

### Batch processing

Events are validated independently.

A batch can contain valid, duplicate, and invalid events. Valid events are processed, duplicates are ignored, and invalid events are reported with their reasons.

A database transaction is used for each batch. If a database operation fails, the batch transaction is rolled back.

### Out-of-order events

Events are stored independently without requiring a particular event sequence.

For example, an `opened` event can arrive before its corresponding `delivered` event.

The service accepts valid events based on their individual fields and does not reject them based on arrival order.

### Event timestamps

The provider timestamp represents when the event occurred, not when Relay received it.

Timestamps are normalized to UTC before storage. Daily delivery statistics use UTC dates.

### Statistics calculation

Statistics are calculated directly from the stored events table using SQL aggregate queries.

The service does not maintain separate in-memory counters, avoiding inconsistencies caused by duplicate event ingestion.

---

## 10. Testing

Start the server:

```bash
go run .
```

Test the ingestion endpoint using Postman or curl:

```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '[{"event_id":"evt_001","campaign_id":"cmp_test","contact_id":"ct_001","type":"sent","timestamp":"2026-08-10T09:00:00Z"}]'
```

Retrieve campaign statistics:

```bash
curl http://localhost:8080/campaigns/cmp_test/stats
```

### Recommended test cases

1. Successfully ingest a valid event.
2. Submit the same event ID twice.
3. Submit an event with missing required fields.
4. Submit an unsupported event type.
5. Submit an invalid timestamp.
6. Submit a batch containing both valid and invalid events.
7. Submit events belonging to multiple campaigns.
8. Submit multiple open events from the same contact.
9. Submit out-of-order events.
10. Send concurrent requests containing the same event ID.
11. Retrieve statistics for a campaign with no events.
12. Verify daily delivery buckets across UTC date boundaries.

---

## 11. Future Improvements

For larger event volumes, the following improvements could be considered:

- Migrate from SQLite to PostgreSQL.
- Introduce asynchronous processing with a message queue.
- Use background workers for event processing.
- Maintain pre-aggregated campaign statistics.
- Partition event storage for high-volume workloads.
- Add structured logging, monitoring, and API rate limiting.

These improvements are not required for the current assignment scope.

---

## 12. Scope and Assumptions

- The service handles event ingestion and campaign statistics only.
- Actual email, SMS, and push notification delivery is handled by third-party providers.
- `event_id` is assumed to be globally unique across providers.
- `unique_opens` is calculated using distinct contacts per campaign.
- Statistics are calculated from successfully stored events.
- The service uses synchronous HTTP request processing and SQLite persistence.
