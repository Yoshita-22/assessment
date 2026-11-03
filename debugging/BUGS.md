# BUGS.md

## Bug 1: Duplicate event IDs across batches

**What the bug is:**

The program could count the same event more than once if it appeared in different batches.

**Why it happens:**

The `seen` map was declared inside `processBatch()`. Every time a new batch was processed, a new empty map was created. Therefore, the program could detect duplicates within a batch but not across the entire input file.

**Fix:**

Moved the event ID tracking map outside `processBatch()` and declared it globally as `seenEventIDs`.

Now, the same event ID is remembered even when it appears in a later batch.

**How I verified the fix:**

Checked the deduplication logic to ensure that event IDs remain recorded across batch boundaries. The final output was compared with `expected_output.txt`.

---

## Bug 2: Unique opens were tracked globally instead of per campaign

**What the bug is:**

The program incorrectly treated a contact who opened messages in two different campaigns as one unique open across the entire system.

**Why it happens:**

The original `openedBy` map used only `contact_id` as its key.

For example, if contact `ct_01` opened campaign A and campaign B, the second open was incorrectly ignored.

**Fix:**

Introduced a `campaignContact` struct containing both `CampaignID` and `ContactID`.

The `openedBy` map now uses this struct as its key.

This ensures that the same contact is counted once per campaign, while still allowing that contact to be counted separately in another campaign.

**How I verified the fix:**

Checked the logic with a contact opening two different campaigns. Each campaign should count that contact as one unique open.

---

## Bug 3: Daily delivered buckets used local time instead of UTC

**What the bug is:**

Delivered events could be assigned to the wrong calendar day because the program used the machine's local timezone.

**Why it happens:**

The original implementation used:

`ev.Timestamp.Local().Format("2006-01-02")`

The local timezone can differ across machines. For example, an event occurring at 23:30 UTC on August 10 becomes 05:00 on August 11 in India.

This caused the event to be assigned to August 11 instead of August 10.

**Fix:**

Changed the date calculation to:

`ev.Timestamp.UTC().Format("2006-01-02")`

This ensures that daily buckets always use the event's UTC date, regardless of the machine's timezone.

**How I verified the fix:**

Checked an event occurring near midnight UTC. Its daily bucket should use the UTC calendar date rather than the local calendar date.

---

## Bug 4: Concurrent counter updates caused a data race

**What the bug is:**

Multiple workers could update the same campaign counters simultaneously, resulting in lost increments and inconsistent statistics.

**Why it happens:**

The program uses eight goroutines to process events concurrently.

Operations such as `cs.Opened++` are not atomic. Two workers could read the same counter value and overwrite each other's updates.

For example, if the counter is 10, two workers might both read 10 and write 11. The expected value is 12, but the actual value becomes 11.

**Fix:**

Introduced a `sync.Mutex` named `statsMu`.

The `apply()` function now locks the mutex before updating the shared counters and unlocks it after the update.

This ensures that only one worker modifies the counters at a time.

**How I verified the fix:**

Used Go's race detector:

`go run -race . events.jsonl`

The race detector helps identify concurrent access to shared memory that is not properly synchronized.

---

## Final verification

Ran the debugging program using:

`go run . events.jsonl`

Compared the generated output against `expected_output.txt`.

Also used:

`go run -race . events.jsonl`

to check for data races.

The expected result is that the program produces exactly the same output as `expected_output.txt` on every run, with no race detector warnings.