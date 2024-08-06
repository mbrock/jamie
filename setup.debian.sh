#!/bin/bash

# Exit immediately if a command exits with a non-zero status
set -e

# Create system user 'jamie'
sudo adduser --system --no-create-home --disabled-password --disabled-login jamie

# Install PostgreSQL if not already installed
sudo apt-get update
sudo apt-get install -y postgresql

# Create PostgreSQL user and database
sudo -u postgres psql -c "CREATE USER jamie WITH PASSWORD 'jamie';"
sudo -u postgres psql -c "CREATE DATABASE jamie OWNER jamie;"

# Run the db_init.sql file
sudo -u postgres psql -d jamie -f db/db_init.sql

echo "Setup completed successfully!"
