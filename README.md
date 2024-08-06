# Jamie - Your Discord Voice Channel's New Best Friend 🎙️🤖

Welcome to Jamie, the Discord bot that's all ears (and text)! 🦻📝 Jamie is here
to join your voice channels, eavesdrop on your conversations (in a totally
non-creepy way), and provide real-time transcriptions that'll make you wonder if
it's secretly a court stenographer in disguise.

## Features (or "Why Jamie is Cooler Than Your Human Friends")

- 🎭 Joins Discord voice channels (No social anxiety here!)
- 🎬 Records and processes audio in real-time (Like a spy, but legal)
- 💾 Stores audio data as Opus packets in PostgreSQL (For those who like their
  data fancy)
- 🚀 Real-time transcription using Speechmatics (Faster than your average
  typist)
- 🧠 Offline transcription and analysis using Google's Gemini multimodal
  generative AI model (It's like having Einstein in your server)
- 📚 Stores and retrieves transcriptions (Because scrolling through chat is so
  last year)
- 📊 Generates voice activity reports (Find out who's the chatterbox in your
  server)
- 🌐 Provides a web interface to view transcriptions (For when reading Discord
  is too mainstream)

## The Secret Sauce: Aider and Claude 3.5 Sonnet 🧙‍♂️✨

Here's where it gets really interesting! Jamie isn't just any bot - it's a bot
with a pedigree. We use the Aider (AI-Driven Development Revolution) coding
agent with Claude 3.5 Sonnet for most of our development work. It's like having
a team of AI developers working 24/7, fueled by nothing but electricity and the
occasional existential crisis.

Curious about Aider? Check out [aider.chat](https://aider.chat) and prepare to
have your mind blown! 🤯

## Architecture (or "How Jamie Keeps It All Together")

Jamie is like a well-oiled machine, with several key components working in
harmony:

1. **Discord Bot**: The face of the operation, charming its way into your voice
   channels.
2. **Audio Processing**: Turning your dulcet tones into cold, hard data.
3. **Transcription Engine**: Where the magic happens - Speechmatics for
   real-time transcription, and Google's Gemini for when we need to get fancy.
4. **Database**: PostgreSQL, because Jamie likes its data like it likes its
   coffee - robust and relational.
5. **Web Interface**: For when you want to relive those voice chat moments, but
   in text form.

## Commands (or "How to Boss Jamie Around")

Jamie is at your beck and call with these commands:

- `listen`: "Hey Jamie, time to earn your keep!"
- `http`: "Jamie, show me what you've got on the web."
- `packets`: "Jamie, what's the latest in the world of Opus packets?"
- `packetInfo`: "Jamie, tell me everything you know about these specific
  packets."
- `report`: "Jamie, who's been talking too much?"
- `transcribe`: "Jamie, write down everything everyone says, and make it
  snappy!"
- `stream`: "Jamie, show me the transcriptions in real-time, I don't want to
  miss a thing!"

## The Codebase (or "Jamie's Brain, Dissected")

Jamie's inner workings are neatly organized into these packages:

- `bot`: Where Jamie learns how to be a good Discord citizen.
- `db`: Jamie's memory bank, powered by sqlc for type-safe SQL goodness.
- `snd`: Where Jamie learns to appreciate the finer points of audio processing.
- `tts`: Jamie's notebook, where it jots down everything it hears.
- `gemini`: Jamie's connection to the all-knowing Google gods.
- `speechmatics`: Jamie's ear training module.

Key files you might want to buy dinner first:

- `main.go`: The big boss, where it all begins.
- `db/queries.sql`: Jamie's favorite bedtime stories, in SQL form.
- `tts/transcript_builder.go`: Where Jamie practices its handwriting.
- `bot/bot.go`: Jamie's Discord etiquette guide.

## Database Schema (or "Jamie's Filing System")

Jamie keeps track of everything in its PostgreSQL brain, including:

- Discord sessions (Jamie's social calendar)
- SSRC mappings (Jamie's "who's who" guide)
- Opus packets (Jamie's audio diary)
- Voice state events (Jamie's mood ring)
- Bot voice joins (Jamie's party crasher log)
- Transcription sessions, segments, and words (Jamie's actual transcriptions)
- Uploaded files (Jamie's scrapbook)

## Project Status and Vision (or "Jamie's Dreams of Electric Sheep")

Jamie is young, ambitious, and full of potential! While it's still learning the
ropes, the end goal is for Jamie to be the ultimate Discord server sidekick:

- 🕵️‍♂️ Keeping track of conversations like a nosy but helpful neighbor
- 🔍 Finding and referencing previous discussions faster than you can say
  "search function"
- 🧠 Proactively helping users by recognizing when they're looking for
  information (It's basically psychic, but with AI)

We're aiming for Jamie to be that helpful presence that steps in when needed,
like a digital butler with impeccable timing. And thanks to its pluggable
architecture, Jamie can grow and adapt faster than a chameleon on a disco dance
floor!

## Technologies and Tools (or "Jamie's Toolbox")

- Go 1.20 or later (Because Jamie likes to go fast)
- PostgreSQL 16 for data storage (Jamie's got a thing for elephants, especially the latest and greatest)
- sqlc for type-safe SQL in Go (Because typos are so last century)
- FFmpeg for audio conversion (Jamie's universal translator for audio)
- Discord API via discordgo library (Jamie's Discord phrasebook)
- Google Cloud API for Gemini (Jamie's hotline to the AI overlords)
- Speechmatics API (Jamie's ear-to-text converter)
- Docker and Docker Compose (Jamie's containerization toolkit)

## Setup (or "Teaching Jamie to Sit and Stay")

### Traditional Setup

1. Clone the repository:

   ```
   git clone https://github.com/mbrock/jamie
   cd jamie
   ```

2. Initialize the project:

   ```
   make init
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

### Docker Compose Setup (or "Jamie's Containerized Adventure")

For an easy and manageable deployment, we've provided a Docker Compose configuration. This setup includes PostgreSQL 16 database and all the necessary Jamie services.

#### Prerequisites

- Docker
- Docker Compose

#### Getting Started with Docker Compose

1. Create a `.env` file in the root directory with the following content:

   ```
   POSTGRES_PASSWORD=your_secure_postgres_password
   DISCORD_TOKEN=your_discord_bot_token
   GEMINI_API_KEY=your_google_cloud_api_key
   SPEECHMATICS_API_KEY=your_speechmatics_api_key
   ```

2. Build and start the services:

   ```
   docker-compose up -d
   ```

   This command will start the following services:
   - PostgreSQL database
   - Jamie Listen service (main bot functionality)
   - Jamie Transcribe service
   - Jamie Serve service (HTTP server)

3. To view logs for a specific service:

   ```
   docker-compose logs -f jamie-listen
   ```

   Replace `jamie-listen` with `jamie-transcribe` or `jamie-serve` to view logs for other services.

4. To stop the services:

   ```
   docker-compose down
   ```

#### Managing Services

- To restart a specific service:

  ```
  docker-compose restart jamie-listen
  ```

- To stop a specific service:

  ```
  docker-compose stop jamie-transcribe
  ```

- To start a stopped service:

  ```
  docker-compose start jamie-serve
  ```

With this Docker Compose setup, you can easily manage and deploy Jamie on your server. Each service is isolated in its own container, making it simple to start, stop, and monitor individual components of the system.

### Systemd Setup (or "Jamie's System Integration")

For a more integrated setup on Linux systems using systemd, follow these steps:

1. Build the Jamie binary and place it in `/usr/local/bin/`:

   ```
   make
   sudo cp jamie /usr/local/bin/
   ```

2. Create a jamie user and group:

   ```
   sudo useradd -r -s /bin/false jamie
   ```

3. Create a directory for Jamie's configuration:

   ```
   sudo mkdir /etc/jamie
   sudo cp .env /etc/jamie/jamie.env
   sudo chown -R jamie:jamie /etc/jamie
   sudo chmod 600 /etc/jamie/jamie.env
   ```

4. Install the systemd service files:

   ```
   make install-systemd
   ```

5. Reload systemd to recognize the new service files:

   ```
   sudo systemctl daemon-reload
   ```

6. Enable and start the Jamie services:

   ```
   sudo systemctl enable jamie-listen jamie-transcribe jamie-serve
   sudo systemctl start jamie-listen jamie-transcribe jamie-serve
   ```

7. Check the status of the services:

   ```
   sudo systemctl status jamie-listen jamie-transcribe jamie-serve
   ```

To view logs for a specific service:

```
sudo journalctl -u jamie-listen
```

Replace `jamie-listen` with `jamie-transcribe` or `jamie-serve` to view logs for other services.

This systemd setup allows Jamie to run as a system service, automatically starting on boot and managed through the systemd interface.

## Usage (or "Taking Jamie for a Walk")

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

## Contributing (or "Joining Jamie's Pack")

Want to help Jamie grow? Contributions are welcome! Just remember:

1. Fork the repository and create your branch from `main`.
2. Write clear, commented code (Jamie likes to understand what's going on).
3. Ensure any new features are properly tested (Jamie doesn't like surprises).
4. Update the README.md if you've made significant changes (Help keep Jamie's
   diary up to date).

## License

This project is licensed under the GNU Affero General Public License v3.0 or
later (AGPL-3.0-or-later) - see the LICENSE file for details. Because Jamie
believes in freedom, especially the freedom to eavesdrop on your Discord
conversations (with your permission, of course).

Now go forth and let Jamie revolutionize your Discord experience! 🚀🎉
