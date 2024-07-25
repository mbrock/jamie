
## Time Parsing and Formatting

To prevent issues with time parsing and formatting across the application, follow these guidelines:

1. Use a consistent time format for all database operations and API responses.
2. Prefer RFC3339 format (e.g., "2006-01-02T15:04:05Z07:00") for storing and transmitting timestamps.
3. When parsing time from the database or external sources, always use the format that matches the source.
4. When formatting time for display or storage, use the appropriate format for the context.

Example:

```go
// Parsing time from database (assuming RFC3339 format)
timestamp, err := time.Parse(time.RFC3339, timestampStr)

// Formatting time for display
formattedTime := timestamp.Format("2006-01-02 15:04:05")

// Storing time in the database
_, err = db.Exec("INSERT INTO table (timestamp) VALUES (?)", time.Now().Format(time.RFC3339))
```

By following these guidelines, we can ensure consistency and prevent parsing errors across the application.
