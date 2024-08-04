# Jamie - Discord Voice Channel Transcription Bot

Jamie is a Discord bot designed to join voice channels, record audio, and provide real-time transcription of conversations. It's built with Go and uses various technologies to process and analyze audio data.

## Features

- Join Discord voice channels
- Record and process audio in real-time
- Store audio data as Opus packets in PostgreSQL
- Transcribe conversations using either Google's Gemini API or Speechmatics
- Store and retrieve transcriptions
- Generate voice activity reports
- Provide a web interface to view transcriptions

## Architecture and Key Components

Jamie is structured around several key components:

1. **Discord Bot**: Handles interactions with Discord, joining voice channels, and capturing audio.
2. **Audio Processing**: Captures Opus packets and stores them in PostgreSQL.
3. **Transcription Engine**: Uses Speechmatics or Google's Gemini API to transcribe audio.
4. **Database**: PostgreSQL stores Opus packets, transcriptions, and other metadata.
5. **Web Interface**: Provides a way to view and interact with transcriptions.

## Commands

Jamie offers several commands:

- `listen`: Starts the Discord bot and begins listening in voice channels.
- `http`: Starts an HTTP server to display transcripts.
- `packets`: Listens for new Opus packets and displays information about them.
- `packetInfo`: Retrieves information about Opus packets for a given SSRC and time range.
- `report`: Generates a voice activity report for a specified time range.
- `transcribe`: Starts transcribing incoming audio, creating a separate transcription session per user.
- `stream`: Displays a terminal UI showing real-time transcriptions.

## Codebase Navigation

The project is organized into several packages:

- `bot`: Contains the Discord bot logic.
- `db`: Database interactions and SQL queries (using sqlc).
- `snd`: Audio processing and Opus packet handling.
- `tts`: Transcription logic and UI rendering.
- `gemini`: Integration with Google's Gemini API.
- `speechmatics`: Integration with Speechmatics API.

Key files to be familiar with:

- `main.go`: Entry point of the application, defines commands.
- `db/queries.sql`: SQL queries used by sqlc to generate Go code.
- `tts/transcript_builder.go`: Handles building and rendering transcripts.
- `bot/bot.go`: Core Discord bot functionality.

## Technologies and Tools

- Go 1.20 or later
- PostgreSQL for data storage
- sqlc for type-safe SQL in Go
- FFmpeg for audio conversion
- Discord API (via discordgo library)
- Google Cloud API (for Gemini)
- Speechmatics API

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

To start transcribing audio:
```
./jamie transcribe
```

To view real-time transcriptions in the terminal:
```
./jamie stream
```

For more commands and options, run:
```
./jamie --help
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request. When contributing, please:

1. Fork the repository and create your branch from `main`.
2. Write clear, commented code.
3. Ensure any new features are properly tested.
4. Update the README.md if you've made significant changes.

## License

This project is licensed under the GNU Affero General Public License v3.0 or later (AGPL-3.0-or-later) - see the LICENSE file for details.
