# **AI Usage Disclosure**

## **1\. Tools Used**

* **ChatGPT:** Used as an AI assistant throughout the assignment.  
* **Visual Studio Code:** Used as the development environment.  
* **Go documentation and SQLite documentation:** Used as technical references when setting up and understanding the technologies.

## **2\. What I Used AI For**

I used ChatGPT mainly for understanding the assignment requirements and planning the implementation.

Specific areas where I used it:

* Understanding the event-processing workflow and the role of Relay.  
* Understanding batch processing and partial success when a batch contains valid, invalid, and duplicate events.  
* Deciding how to identify duplicate events using `event_id`.  
* Understanding late-arriving and out-of-order events.  
* Planning the SQLite database schema and identifying appropriate constraints and indexes.  
* Understanding how concurrent HTTP requests can cause duplicate insertions and how database uniqueness constraints prevent them.  
* Planning the backend folder structure and separation of handlers, services, repositories, and database logic.  
* Preparing the scale memo and reasoning about how the system could evolve as traffic increases.  
* Understanding possible reasons for changes in campaign statistics and how to investigate them.

## **3\. One AI Suggestion I Rejected or Changed**

AI initially suggested a more elaborate backend structure with several separate folders and files.

I decided to keep the initial implementation simpler because the assignment has a limited time window of approximately four hours.

I will reuse the provided starter code wherever possible and introduce additional layers only when they provide a clear benefit.

**Reason:** I want to prioritize a working, understandable implementation instead of spending too much time on project structure.

## **4\. One Thing AI Helped Me Understand**

AI helped me understand why duplicate event handling must be enforced at the database level rather than relying only on application-level checks.

For example, two requests can arrive simultaneously with the same `event_id`. Both requests might check the database and find that the event does not exist. If both then insert it, duplicate records could occur.

Using a database primary key or unique constraint on `event_id` ensures that only one insertion succeeds.

This helped me understand the importance of database constraints when handling concurrent requests.

## 5. AI-Generated Content That I Had to Debug

I used AI assistance to generate and understand parts of the initial implementation. The generated code was treated as a starting point rather than a final solution.

The AI-assisted implementation included:

- HTTP handlers for `POST /events` and `GET /campaigns/{campaign_id}/stats`.
- Event validation and batch processing logic.
- SQLite database operations and duplicate event handling.
- JSON request and response structures.
- Error handling for invalid events and database operations.

I reviewed the generated code and worked on understanding how each component functions, including database transactions, unique constraints, HTTP handlers, and event processing.

I am responsible for verifying the final implementation. I will run the application, test valid and invalid requests, check duplicate handling, verify campaign statistics, and fix any issues identified during testing.

AI assistance was used as a learning and development aid, and I will ensure that I can explain the implementation and the design decisions independently.


## **6\. Verification and Responsibility**

I am responsible for the final implementation.

I will run the application, test the important behaviours, inspect the database results, and ensure that I can explain the implementation and design decisions during the follow-up interview.

AI suggestions are treated as guidance rather than automatically accepted solutions.

