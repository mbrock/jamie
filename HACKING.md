## Time Parsing and Formatting

To prevent issues with time parsing and formatting across the application,
follow these guidelines:

1. Use a consistent time format for all database operations and API responses.
2. SQLite default time format for timestamps seems to be "2024-07-24 12:42:07".
3. When parsing time from the database or external sources, always use the
   format that matches the source.
4. When formatting time for display or storage, use the appropriate format for
   the context.
