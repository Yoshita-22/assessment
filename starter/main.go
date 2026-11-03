package main

import (
	"campaign-events/database"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// IncomingEvent represents an event received from a provider.
type IncomingEvent struct {
	EventID    string          `json:"event_id"`
	CampaignID string          `json:"campaign_id"`
	ContactID  string          `json:"contact_id"`
	Type       string          `json:"type"`
	Timestamp  string          `json:"timestamp"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

// EventError describes why an individual event was rejected.
type EventError struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Reason  string `json:"reason"`
}

// BatchResponse summarizes the result of processing a batch.
type BatchResponse struct {
	Received   int          `json:"received"`
	Accepted   int          `json:"accepted"`
	Duplicates int          `json:"duplicates"`
	Rejected   int          `json:"rejected"`
	Errors     []EventError `json:"errors,omitempty"`
}

// CampaignStats contains aggregated campaign statistics.
type CampaignStats struct {
	CampaignID     string          `json:"campaign_id"`
	Sent           int             `json:"sent"`
	Delivered      int             `json:"delivered"`
	Opened         int             `json:"opened"`
	Clicked        int             `json:"clicked"`
	UniqueOpens    int             `json:"unique_opens"`
	DailyDelivered []DailyDelivery `json:"daily_delivered"`
}

// DailyDelivery represents delivered events for one UTC date.
type DailyDelivery struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func main() {
	// Initialize SQLite database.
	db, err := database.InitDB()
	if err != nil {
		log.Fatal("Database initialization failed:", err)
	}
	defer db.Close()

	log.Println("Database connected successfully!")

	// Register HTTP routes.
	mux := http.NewServeMux()

	mux.HandleFunc("POST /events", handlePostEvents(db))
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", handleGetStats(db))

	addr := ":8080"

	fmt.Printf("Listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// ----------------------------------------------------
// POST /events
// ----------------------------------------------------

// handlePostEvents ingests a JSON array of events.
func handlePostEvents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// Limit request body to 10 MB.
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		defer r.Body.Close()

		// Decode the array into RawMessage.
		// This allows individual events to be validated separately.
		var rawEvents []json.RawMessage

		decoder := json.NewDecoder(r.Body)

		if err := decoder.Decode(&rawEvents); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "request must contain a valid JSON array",
			})
			return
		}

		// Reject additional JSON after the array.
		var extra interface{}

		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "request must contain only one JSON array",
			})
			return
		}

		if rawEvents == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "request body must be a JSON array",
			})
			return
		}

		result := BatchResponse{
			Received: len(rawEvents),
			Errors:   make([]EventError, 0),
		}

		// Start a database transaction for the batch.
		tx, err := db.Begin()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not start database transaction",
			})
			return
		}

		committed := false

		// Roll back if the transaction is not committed.
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		// event_id is a PRIMARY KEY.
		// Existing event IDs will not be inserted again.
		insertQuery := `
			INSERT INTO events
				(event_id, campaign_id, contact_id, type, timestamp, metadata)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(event_id) DO NOTHING
		`

		stmt, err := tx.Prepare(insertQuery)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not prepare database statement",
			})
			return
		}
		defer stmt.Close()

		// Process every event independently.
		for index, raw := range rawEvents {

			var event IncomingEvent

			if err := json.Unmarshal(raw, &event); err != nil {
				rejectEvent(&result, index, "", "invalid event JSON")
				continue
			}

			// Validate and normalize required fields.
			event.EventID = strings.TrimSpace(event.EventID)
			event.CampaignID = strings.TrimSpace(event.CampaignID)
			event.ContactID = strings.TrimSpace(event.ContactID)

			if event.EventID == "" {
				rejectEvent(&result, index, event.EventID,
					"event_id is required")
				continue
			}

			if event.CampaignID == "" {
				rejectEvent(&result, index, event.EventID,
					"campaign_id is required")
				continue
			}

			if event.ContactID == "" {
				rejectEvent(&result, index, event.EventID,
					"contact_id is required")
				continue
			}

			// Validate event type.
			switch event.Type {
			case "sent", "delivered", "opened", "clicked":
				// Valid event type.
			default:
				rejectEvent(&result, index, event.EventID,
					"invalid event type")
				continue
			}

			// Validate provider timestamp.
			eventTime, err := time.Parse(time.RFC3339Nano, event.Timestamp)
			if err != nil {
				rejectEvent(&result, index, event.EventID,
					"invalid timestamp; expected RFC3339 format")
				continue
			}

			// Normalize event timestamp to UTC.
			timestamp := eventTime.UTC().Format(time.RFC3339Nano)

			// Store metadata as JSON text.
			var metadata interface{}

			if len(event.Metadata) > 0 &&
				string(event.Metadata) != "null" {
				metadata = string(event.Metadata)
			}

			// Insert event into SQLite.
			res, err := stmt.Exec(
				event.EventID,
				event.CampaignID,
				event.ContactID,
				event.Type,
				timestamp,
				metadata,
			)

			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "database insert failed",
				})
				return
			}

			rowsAffected, err := res.RowsAffected()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "could not determine insert result",
				})
				return
			}

			if rowsAffected == 0 {
				result.Duplicates++
			} else {
				result.Accepted++
			}
		}

		// Close the prepared statement before committing.
		if err := stmt.Close(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not close database statement",
			})
			return
		}

		// Commit the successfully processed events.
		if err := tx.Commit(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not commit events",
			})
			return
		}

		committed = true

		writeJSON(w, http.StatusOK, result)
	}
}

// rejectEvent records an invalid event without stopping the batch.
func rejectEvent(
	result *BatchResponse,
	index int,
	eventID string,
	reason string,
) {
	result.Rejected++

	result.Errors = append(result.Errors, EventError{
		Index:   index,
		EventID: eventID,
		Reason:  reason,
	})
}

// ----------------------------------------------------
// GET /campaigns/{campaignID}/stats
// ----------------------------------------------------

// handleGetStats returns aggregated statistics for a campaign.
func handleGetStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		campaignID := r.PathValue("campaignID")

		if strings.TrimSpace(campaignID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "campaign_id is required",
			})
			return
		}

		stats := CampaignStats{
			CampaignID:     campaignID,
			DailyDelivered: make([]DailyDelivery, 0),
		}

		// Calculate overall campaign statistics.
		query := `
			SELECT
				COUNT(CASE WHEN type = 'sent' THEN 1 END),
				COUNT(CASE WHEN type = 'delivered' THEN 1 END),
				COUNT(CASE WHEN type = 'opened' THEN 1 END),
				COUNT(CASE WHEN type = 'clicked' THEN 1 END),
				COUNT(DISTINCT CASE
					WHEN type = 'opened' THEN contact_id
				END)
			FROM events
			WHERE campaign_id = ?
		`

		err := db.QueryRow(query, campaignID).Scan(
			&stats.Sent,
			&stats.Delivered,
			&stats.Opened,
			&stats.Clicked,
			&stats.UniqueOpens,
		)

		if err != nil {
			log.Printf("stats query failed: %v", err)

			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not retrieve campaign statistics",
			})
			return
		}

		// Calculate delivered events grouped by UTC date.
		// POST /events stores timestamps normalized to UTC.
		dailyQuery := `
			SELECT
				date(timestamp) AS event_date,
				COUNT(*) AS delivered_count
			FROM events
			WHERE campaign_id = ?
			  AND type = 'delivered'
			GROUP BY date(timestamp)
			ORDER BY date(timestamp) ASC
		`

		rows, err := db.Query(dailyQuery, campaignID)
		if err != nil {
			log.Printf("daily stats query failed: %v", err)

			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not retrieve daily delivery statistics",
			})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var daily DailyDelivery

			if err := rows.Scan(&daily.Date, &daily.Count); err != nil {
				log.Printf("daily stats scan failed: %v", err)

				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "could not read daily delivery statistics",
				})
				return
			}

			stats.DailyDelivered = append(stats.DailyDelivered, daily)
		}

		if err := rows.Err(); err != nil {
			log.Printf("daily stats iteration failed: %v", err)

			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "could not read daily delivery statistics",
			})
			return
		}

		writeJSON(w, http.StatusOK, stats)
	}
}

// ----------------------------------------------------
// Shared JSON response helper
// ----------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
