#!/bin/bash

set -ex

if ! id -u jamie; then
  sudo adduser --disabled-password jamie \
    --gecos "Jamie" \
    --home /home/jamie \
    --shell /bin/bash
fi

if ! sudo -u postgres psql -c "SELECT 1 FROM pg_user WHERE usename = 'jamie';" | grep -q 1; then
  sudo -u postgres psql -c "CREATE USER jamie WITH PASSWORD 'jamie';"
fi

sudo -u postgres createdb -O jamie jamie || echo "Database already exists"
# sudo -u jamie psql -f db/db_init.sql 

for var in DISCORD_TOKEN OPENAI_API_KEY DEEPGRAM_API_KEY ANTHROPIC_API_KEY GEMINI_API_KEY SPEECHMATICS_API_KEY ELEVENLABS_API_KEY; do
  jamie config set "$var" "${!var}"
done