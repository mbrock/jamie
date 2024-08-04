# Jamie - Discord Voice Channel Transcription Bot

Jamie is a Discord bot designed to join voice channels, record audio, and
provide real-time transcription of conversations. It's built with Go and uses
various technologies to process and analyze audio data.

## Features

- Join Discord voice channels
- Record and process audio in real-time
- Transcribe conversations using either Google's Gemini API or Speechmatics
- Store and retrieve transcriptions
- Generate voice activity reports
- Provide a web interface to view transcriptions

## Dependencies

- Go 1.20 or later
- PostgreSQL database
- FFmpeg (for audio conversion)
- Discord Bot Token
- Google Cloud API key (for Gemini API)
- Speechmatics API key (optional, for alternative transcription service)

## Setup

1. Clone the repository:

   ```
   git clone https://github.com/mbrock/jamie
   cd jamie
   ```

2. Install dependencies:

   ```
   go mod tidy
   ```

3. Set up your PostgreSQL database and run the initialization script:

   ```
   psql -d jamie -f db/db_init.sql
   ```

4. Create a `.env` file in the root directory with the following content:

   ```
   DATABASE_URL=postgres://username:password@localhost:5432/jamie
   DISCORD_TOKEN=your_discord_bot_token
   GEMINI_API_KEY=your_google_cloud_api_key
   SPEECHMATICS_API_KEY=your_speechmatics_api_key
   ```

5. Build the project:
   ```
   make
   ```

## Usage

To start the bot and listen in Discord voice channels:

```
./jamie listen
```

To start the HTTP server for viewing transcripts:

```
./jamie http
```

For more commands and options, run:

```
./jamie --help
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the GNU Affero General Public License v3.0 or
later (AGPL-3.0-or-later) - see the LICENSE file for details.
