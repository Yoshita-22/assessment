# **Design Decisions and Assumptions**

## **1\. What makes two events the same?**

I will consider two events to be duplicates if they have the same `event_id`.

* The `event_id` is assigned by the provider and uniquely identifies an event.  
* Even if the same event is delivered multiple times, it should be stored only once.  
* Two events with different `event_id` values will be treated as separate events, even if their `campaign_id`, `contact_id`, `type`, and `timestamp` are identical.

**Example:**

Suppose the provider sends this event twice:

```
{
  "event_id": "evt_001",
  "campaign_id": "cmp_001",
  "contact_id": "ct_001",
  "type": "opened",
  "timestamp": "2026-08-10T09:15:04Z"
}
```

Both requests have the same `event_id`, so the second occurrence is considered a duplicate.

I will enforce this using a `UNIQUE` constraint or primary key on `event_id` in the database. This ensures duplicate events cannot be stored, even when requests arrive concurrently.

**Assumption:** The provider guarantees that an `event_id` identifies one event globally. If this guarantee is not valid, the uniqueness scope would need to include the provider identifier.

## **2\. What should the API return?**

The API should clearly distinguish between successfully accepted events, duplicates, and invalid events.

### **A. Duplicate event**

If an event with an existing `event_id` is received:

* Do not insert it again.  
* Count it as a duplicate.  
* Return a successful HTTP response for the batch, with the duplicate reflected in the result.

### **B. Invalid event**

An event is invalid if it fails basic validation. For example:

* Missing `event_id`.  
* Missing `campaign_id` or `contact_id`.  
* Unsupported event type.  
* Missing or invalid timestamp.  
* Invalid JSON structure.

Invalid events should not be stored. The API should report why each rejected event failed validation.

### **C. Batch containing both valid and invalid events**

I will process each event independently instead of rejecting the entire batch because of one invalid event.

For example, consider a batch containing five events:

| Event | Result |
| ----- | ----- |
| Event 1 | Accepted |
| Event 2 | Accepted |
| Event 3 | Rejected — invalid timestamp |
| Event 4 | Duplicate |
| Event 5 | Accepted |

The response could be:

```
{
  "received": 5,
  "accepted": 3,
  "duplicates": 1,
  "rejected": 1,
  "errors": [
    {
      "index": 2,
      "reason": "invalid timestamp"
    }
  ]
}
```

I will use HTTP `200 OK` when the batch has been processed, including when some individual events are rejected. The response body communicates the individual outcomes.

For a malformed request body that cannot be interpreted as a batch at all, I will return `400 Bad Request`.

**Reason:** Partial batch processing prevents one bad event from causing otherwise valid events to be lost.

## **3\. What does "how many opened" mean?**

For this implementation, I define the statistics as the **number of unique events recorded for each event type in a campaign**.

For example, suppose a campaign has these events:

| Event ID | Contact ID | Type |
| ----- | ----- | ----- |
| evt\_001 | ct\_001 | sent |
| evt\_002 | ct\_001 | delivered |
| evt\_003 | ct\_001 | opened |
| evt\_004 | ct\_002 | opened |
| evt\_005 | ct\_003 | clicked |

The campaign statistics would be:

```
{
  "campaign_id": "cmp_001",
  "sent": 1,
  "delivered": 1,
  "opened": 2,
  "clicked": 1
}
```

Here, `opened: 2` means two unique `opened` events were recorded. It does not necessarily mean two unique contacts opened the message.

If the same contact opens the message multiple times and the provider sends separate events with different event IDs, each event will be counted.

**Assumption:** The assignment does not explicitly define whether counts represent unique contacts or total events. I am choosing unique event counts because the event ID provides a clear deduplication mechanism. Unique-contact metrics can be added separately if the product requires them.

Late-arriving and out-of-order events will still be included in the statistics. I will use the provider's event timestamp to represent when the event happened, rather than treating its arrival time as the event time.

## **4\. How will events be stored, and why?**

I will use SQLite as the persistence layer because the assignment has a small initial workload and SQLite is simple to set up without requiring a separate database server.

I will use one `events` table to store all four event types.

### **Proposed schema**

| Column | Purpose |
| ----- | ----- |
| event\_id | Unique event identifier; primary key |
| campaign\_id | Identifies the campaign |
| contact\_id | Identifies the contact |
| type | sent, delivered, opened, or clicked |
| timestamp | Time the event happened at the provider |
| metadata | Optional additional event information, stored as JSON |
| received\_at | Time Relay received the event |

### **Why one table?**

* All event types share the same basic structure.  
* It avoids creating separate tables for sent, delivered, opened, and clicked events.  
* Campaign statistics can be calculated easily using grouping and counting.  
* It makes querying and retrieving campaign events straightforward.

### **Database constraints and indexes**

* `event_id` will be the primary key to prevent duplicate storage.  
* `campaign_id` will have an index because statistics and event retrieval are performed by campaign.  
* I may use a composite index on `(campaign_id, timestamp)` to support campaign-specific chronological queries.

I will store both `timestamp` and `received_at` because they represent different things:

* `timestamp`: When the provider says the event actually happened.  
* `received_at`: When our service received the event.

This distinction helps investigate delayed events and understand event arrival patterns.

## **5\. What happens when two requests arrive at the same time?**

The service must handle concurrent requests safely.

For example, imagine two provider requests arrive simultaneously with the same event:

* Request A checks whether `evt_001` exists.  
* Request B also checks whether `evt_001` exists.  
* Both might initially find that it does not exist.  
* Both then attempt to insert it.

If duplicate checking is implemented only in application code, both requests could insert the same event.

To prevent this, I will rely on the database's primary key or unique constraint on `event_id`.

The database guarantees that only one insert can succeed. The other insert will be identified as a duplicate and handled accordingly.

I will also:

* Avoid maintaining campaign counts only in shared in-memory variables.  
* Calculate statistics from persisted events.  
* Use database transactions where multiple database operations need to succeed or fail together.  
* Configure SQLite appropriately for concurrent access and handle transient database-busy errors.

**Main principle:** Application-level validation improves error reporting, but database constraints enforce data integrity.

## **6\. Tests: Which behaviours are most important?**

I will prioritize tests that protect the correctness of event ingestion, deduplication, and campaign statistics rather than trying to maximize code coverage.

### **A. Event ingestion tests**

1. **Valid event:** A valid event is accepted and stored.  
2. **Duplicate event:** Sending the same `event_id` twice stores it only once and reports the second occurrence as a duplicate.  
3. **Invalid event:** An event with a missing required field, unsupported type, or invalid timestamp is rejected.  
4. **Mixed batch:** A batch containing valid, invalid, and duplicate events processes each independently. Valid events are not lost because another event is invalid.  
5. **Malformed request:** A request that is not a valid JSON batch returns an appropriate client error.

### **B. Statistics tests**

6. **Correct campaign statistics:** The API returns correct counts for sent, delivered, opened, and clicked events.  
7. **Campaign isolation:** Events belonging to one campaign must not appear in another campaign's statistics.  
8. **Empty campaign:** A campaign with no events returns zero counts for all event types.  
9. **Out-of-order events:** An opened event arriving before a delivered event is still accepted and counted.

### **C. Concurrency test**

10. **Concurrent duplicate requests:** Send multiple simultaneous requests containing the same `event_id`. Verify that the event is stored exactly once and the remaining attempts are treated as duplicates.

### **Why these tests?**

These tests protect the behaviours most likely to cause serious problems:

* Losing valid events because of one bad event in a batch.  
* Counting the same event multiple times.  
* Returning incorrect campaign statistics.  
* Mixing events across campaigns.  
* Rejecting legitimate events just because they arrive late or out of order.  
* Corrupting data when multiple requests arrive simultaneously.

I will prioritize these correctness tests over tests that only verify trivial implementation details.


### What I completed
- Implemented the POST /events endpoint for batch event ingestion.
- Implemented per-event validation and error handling.
- Added duplicate event detection using event_id.
- Added SQLite persistence for incoming events.
- Implemented GET /campaigns/{campaign_id}/stats to retrieve campaign statistics.
- Handled duplicate, invalid, mixed-batch, and out-of-order events.

### What I intentionally skipped
- Did not implement the optional GET /campaigns/{campaign_id}/events endpoint for paginated event retrieval.
- Kept the implementation focused on the core requirements within the available time.
  

# **Scale Memo**

## **Scenario: Growth from a few thousand to 100 million events per day**

My current implementation is designed for a relatively small workload. It uses a Go HTTP server, SQLite for persistence, a single events table, and synchronous processing of incoming batches.

At 100 million events per day, I would not try to solve everything by simply increasing the number of HTTP servers. I would first identify the bottlenecks and evolve the architecture gradually.

### **1\. What breaks first in my current implementation?**

I expect the database to become the first major bottleneck.

* **SQLite write contention:** SQLite supports concurrent reads, but writes are serialized. A large number of incoming requests would compete for the database write lock.  
* **Synchronous request processing:** Every request waits for validation and database insertion to finish. Under heavy traffic, request latency would increase.  
* **Statistics queries:** My current approach calculates campaign statistics from stored events. Scanning a very large events table repeatedly would become expensive.  
* **Single-machine storage:** SQLite is stored locally, so storage capacity, disk I/O, and machine failure become concerns.  
* **Limited horizontal scalability:** Multiple application instances cannot safely scale around one local SQLite database in the same way they could around a shared database service.

Therefore, I would first address database scalability and request-processing throughput.

### **2\. What would I change, and in what order?**

I would make changes incrementally:

1. **Move from SQLite to PostgreSQL:** PostgreSQL provides stronger support for concurrent workloads, connection pooling, indexing, and larger datasets.  
2. **Introduce a message queue:** The API would validate incoming requests and publish accepted events to a queue. Separate Go workers would consume and process them.  
3. **Scale workers horizontally:** I could run multiple worker instances to process events in parallel.  
4. **Optimize statistics:** Instead of repeatedly scanning all raw events, maintain pre-aggregated campaign counters or summary tables.  
5. **Partition large event tables:** Partition events based on time, such as daily or monthly partitions, to make large-scale querying and data retention easier.  
6. **Add caching where useful:** Frequently requested campaign statistics could be cached, with a clear strategy for keeping them updated.

I would measure each stage before introducing additional complexity.

### **3\. Would the API contract or storage design change?**

The basic API endpoints could remain:

* `POST /events`  
* `GET /campaigns/{campaign_id}/stats`

However, the ingestion response may change when processing becomes asynchronous.

Instead of waiting for every event to be inserted, the API could validate the batch, publish it to a queue, and return `202 Accepted` with a batch or request identifier.

The client would understand that the events have been accepted for processing, but statistics might not reflect them immediately.

For storage, I would move from SQLite to PostgreSQL.

I would retain the raw events table as the source of truth, including:

* `event_id`  
* `campaign_id`  
* `contact_id`  
* `type`  
* `timestamp`  
* `metadata`  
* `received_at`

I would also introduce summary tables for campaign statistics so that dashboard queries do not need to scan the entire events table.

### **4\. Would I introduce a queue? What new problems would it create?**

Yes. I would introduce a queue between the HTTP API and event-processing workers.

The flow would become:

Provider → Go API → Message Queue → Event Workers → PostgreSQL → Campaign Statistics API → Dashboard

The queue would absorb traffic spikes and allow workers to process events independently of incoming HTTP requests.

However, a queue introduces new challenges:

* **Duplicate delivery:** A queue may deliver the same message more than once.  
* **Retries:** If a worker fails, the message may be delivered again.  
* **Message ordering:** Events may be processed in a different order from their original occurrence.  
* **Poison messages:** Some messages may repeatedly fail and block normal processing unless isolated.  
* **Queue backlog:** If events arrive faster than workers can process them, the queue will grow.  
* **Acknowledgement failures:** A worker may successfully save an event but fail before acknowledging the queue message.

I would use idempotent event processing, retry policies, dead-letter queues, and queue monitoring to handle these problems.

### **5\. Where can duplicates now sneak in?**

In the current synchronous implementation, the database uniqueness constraint on `event_id` protects against duplicate event insertion.

With a queue-based architecture, duplicates can appear at several stages:

1. The provider retries the same HTTP request because it did not receive a timely response.  
2. The API retries publishing a message because it is uncertain whether the queue received it.  
3. A queue redelivers a message after a worker crashes.  
4. A worker inserts an event successfully but crashes before acknowledging the message.  
5. Multiple workers receive the same event.

I would continue using `event_id` as the idempotency key and enforce its uniqueness in PostgreSQL.

Even if the same event is processed multiple times, only one copy should be stored and counted.

### **6\. How would I keep counters trustworthy?**

I would treat the raw events table as the source of truth.

Campaign counters should be derived from successfully persisted, deduplicated events.

I would maintain summary tables for faster dashboard queries, but I would not treat them as the only source of truth.

Important measures:

* Update an event record and its corresponding aggregate counter transactionally.  
* Ensure duplicate events do not increment counters.  
* Make worker retries idempotent.  
* Periodically compare summary counters against counts calculated from raw events.  
* Rebuild summary counters from raw events if inconsistencies are detected.

This approach helps prevent counters from becoming incorrect due to retries, concurrent processing, or worker failures.

### **7\. What would I monitor?**

I would monitor both application performance and data correctness.

**API metrics**

* Requests per second.  
* Request latency.  
* HTTP error rates.  
* Number of accepted, duplicate, and rejected events.

**Queue and worker metrics**

* Queue depth and message age.  
* Event processing rate.  
* Worker failures and retries.  
* Dead-letter queue size.  
* Number of active workers.

**Database metrics**

* Query and insert latency.  
* Database CPU and disk usage.  
* Connection pool usage.  
* Lock contention.  
* Storage growth.

**Data correctness**

* Duplicate event rate.  
* Rejected event rate.  
* Difference between raw event counts and aggregate counters.  
* Delay between event receipt and availability in campaign statistics.

These metrics would help identify bottlenecks before they cause major failures.

### **8\. What would I deliberately not solve yet?**

I would avoid introducing unnecessary infrastructure before there is a measurable need.

Initially, I would not introduce:

* A complex microservices architecture.  
* Multi-region active-active processing.  
* Distributed transactions.  
* Multiple different databases for different event types.  
* A complicated streaming platform unless queue-based workers are no longer sufficient.

I would first move to PostgreSQL, introduce a queue, scale workers, and optimize statistics.

I would use load tests and production metrics to decide when more advanced infrastructure is necessary.

**Overall approach:** Preserve the simple event-processing model, but gradually separate ingestion, processing, storage, and statistics as traffic grows.

# **Part 5: The Angry Marketer**

## **Problem**

At 10:00, the dashboard shows:

| Metric | Count |
| ----- | ----- |
| Sent | 1,000,000 |
| Delivered | 970,000 |
| Opened | 250,000 |
| Clicked | 20,000 |

At 10:30, it shows:

| Metric | Count |
| ----- | ----- |
| Sent | 1,000,000 |
| Delivered | 975,000 |
| Opened | 248,000 |
| Clicked | 20,000 |

Delivered increased by 5,000, but opened decreased by 2,000.

Before assuming that the dashboard is broken, I would investigate the following possible explanations.

## **1\. Plausible explanations**

### **A. Late-arriving events**

Events may reach Relay long after they actually happened.

For example:

* An opened event may have happened at 09:00 but reached Relay at 10:20.  
* A delivered event may have happened earlier but reached Relay after a provider retry.

This can explain why delivered increased between the two dashboard readings.

### **B. Out-of-order event arrival**

Providers do not guarantee that events arrive in the same order in which they happened.

For example, an opened event might arrive before a delivered event.

Our system accepts events independently, so their arrival order should not affect whether they are counted.

### **C. Different time windows or filters**

The dashboard may be showing different time ranges or filters at 10:00 and 10:30.

For example:

* One view may show lifetime campaign statistics.  
* Another may show statistics for the last 24 hours.  
* A filter may have changed between refreshes.

If the time window changes, counts can increase or decrease.

### **D. Data corrections or event removal**

A provider may send corrected information, or an event may be removed or reclassified.

If the system supports corrections or deletion, a previously counted opened event may no longer contribute to the total.

My current implementation does not implement event correction or deletion, but this is a possible explanation in a larger production system.

### **E. Different counting definitions**

The dashboard might count unique contacts rather than total events.

For example, if one contact opens an email multiple times:

* Total event count may include every opening event.  
* Unique-contact count includes that contact only once.

A change in counting logic or metric definition could produce different numbers.

### **F. Duplicate handling or aggregation issues**

Duplicate events may be handled inconsistently by different parts of the system.

For example:

* The raw events table may contain unique events.  
* A summary table or cache may have been updated incorrectly.  
* An aggregation query may be counting records differently.

### **G. Cached or delayed dashboard data**

The dashboard may use cached statistics or asynchronously updated aggregates.

The 10:00 and 10:30 values may come from different refresh cycles or inconsistent snapshots.

### **H. Query or implementation bugs**

There may be problems in the statistics query, such as:

* Incorrect campaign filtering.  
* Incorrect event-type filtering.  
* A time-zone or timestamp boundary issue.  
* Incorrect aggregation logic.  
* Missing records due to pagination or incomplete processing.

## **2\. What would I check first?**

I would investigate the issue systematically rather than immediately changing the code.

**Step 1: Verify the dashboard filters**

I would confirm that both readings use:

* The same campaign.  
* The same time range.  
* The same time zone.  
* The same counting definition.  
* The same filters.

**Step 2: Check the raw events in the database**

Since my implementation stores individual events, I would query the raw events table for the campaign.

I would compare:

* The number of `delivered` events.  
* The number of `opened` events.  
* The event IDs.  
* The provider timestamps.  
* The received timestamps.

**Step 3: Check for late-arriving events**

I would compare the provider event timestamp with the Relay received timestamp.

This would help identify events that happened earlier but arrived later.

**Step 4: Verify duplicate handling**

I would check whether the database uniqueness constraint on `event_id` is working correctly and whether duplicate events are being counted more than once.

**Step 5: Recalculate the statistics directly**

I would calculate the event counts directly from the raw events table and compare them with the dashboard values.

Since my current implementation calculates statistics from stored events, this helps determine whether the problem is in event storage, the statistics query, or the dashboard.

## **3\. How would I decide whether this is a bug or expected behaviour?**

I would first establish what the metric is supposed to represent.

In my implementation, campaign statistics represent the total number of unique stored events of each type.

For these lifetime-style counts:

* New valid events can increase a count.  
* Duplicate events should not increase a count.  
* Invalid events should not be stored or counted.  
* Late-arriving events can increase a count.  
* Out-of-order arrival should not decrease a count.

Therefore, if the underlying events are only being inserted and never deleted or corrected, the total number of opened events should not decrease.

I would consider the decrease from 250,000 to 248,000 suspicious under my current counting definition.

If the raw database still contains 250,000 opened events but the API returns 248,000, I would investigate the statistics query or API implementation.

If the raw database itself contains fewer opened events, I would investigate whether records were deleted, corrected, or whether the two readings used different filters or definitions.

If the dashboard uses rolling time windows or unique-contact metrics, a decrease may be expected, but that would be a different metric definition from my current implementation.

## **4\. Is the increase in delivered actually suspicious?**

No, not necessarily.

The delivered count increasing from 970,000 to 975,000 is reasonable.

Providers can send delivery events late, retry requests, or deliver events out of order.

For example:

* A message was delivered at 09:45.  
* The provider could not notify Relay immediately.  
* Relay received the event at 10:15.  
* The delivered count increased when the event was stored.

This is expected behaviour in an event-driven system.

The increase would become suspicious only if the same event was counted more than once or if invalid events were included.

## **Conclusion**

I would not immediately assume that the dashboard is broken.

The increase in delivered events can be explained by late-arriving events. However, the decrease in opened events is suspicious under my current implementation because I calculate counts from unique stored event records, and I do not implement deletion or correction of events.

My first step would be to verify the dashboard filters and compare the API statistics with counts calculated directly from the raw events table. This would help me determine whether the issue is expected behaviour, a data consistency problem, or a bug in the statistics calculation.

