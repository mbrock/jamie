#!/bin/bash

# Constants
JAMIE_USERNAME="jamie"
JAMIE_PASSWORD="jamie"
DB_NAME="jamie"

# Logging function
log() {
    echo "LOG ($1): $2"
}

# Run command with optional sudo
run_command() {
    echo "Would run command: $@"
    read -p "Succeed or fail? (s/f): " response
    if [[ $response == "s" ]]; then
        return 0
    else
        return 1
    fi
}

# Confirm function
confirm() {
    read -p "$1 (y/n): " response
    if [[ $response == "y" ]]; then
        return 0
    else
        return 1
    fi
}

# Ensure system user exists
ensure_system_user() {
    if id "$JAMIE_USERNAME" &>/dev/null; then
        log "info" "System user already exists"
    else
        log "info" "Creating system user"
        if run_command useradd -r -s /bin/false "$JAMIE_USERNAME"; then
            log "info" "System user created successfully"
        else
            if confirm "Use sudo?"; then
                if run_command sudo useradd -r -s /bin/false "$JAMIE_USERNAME"; then
                    log "info" "System user created successfully with sudo"
                else
                    log "error" "Failed to create system user"
                    exit 1
                fi
            else
                log "error" "Failed to create system user"
                exit 1
            fi
        fi
    fi
}

# Ensure database user exists
ensure_database_user() {
    if run_command psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$JAMIE_USERNAME'"; then
        log "info" "Database user already exists"
    else
        log "info" "Creating database user"
        if run_command createuser -s "$JAMIE_USERNAME"; then
            if run_command psql -c "ALTER USER $JAMIE_USERNAME WITH PASSWORD '$JAMIE_PASSWORD';"; then
                log "info" "Database user created successfully"
            else
                log "error" "Failed to set database user password"
                exit 1
            fi
        else
            if confirm "Use sudo?"; then
                if run_command sudo -u postgres createuser -s "$JAMIE_USERNAME"; then
                    if run_command sudo -u postgres psql -c "ALTER USER $JAMIE_USERNAME WITH PASSWORD '$JAMIE_PASSWORD';"; then
                        log "info" "Database user created successfully with sudo"
                    else
                        log "error" "Failed to set database user password"
                        exit 1
                    fi
                else
                    log "error" "Failed to create database user"
                    exit 1
                fi
            else
                log "error" "Failed to create database user"
                exit 1
            fi
        fi
    fi
}

# Ensure database exists
ensure_database() {
    if run_command psql -lqt | cut -d \| -f 1 | grep -cw "$DB_NAME"; then
        log "info" "Database already exists"
    else
        log "info" "Creating database"
        if run_command createdb -O "$JAMIE_USERNAME" "$DB_NAME"; then
            log "info" "Database created successfully"
            log "info" "Initializing database schema..."
            if run_command psql -d "$DB_NAME" -f db/db_init.sql; then
                log "info" "Database schema initialized successfully"
            else
                log "error" "Failed to initialize database schema"
                exit 1
            fi
        else
            if confirm "Use sudo?"; then
                if run_command sudo -u postgres createdb -O "$JAMIE_USERNAME" "$DB_NAME"; then
                    log "info" "Database created successfully with sudo"
                    log "info" "Initializing database schema..."
                    if run_command sudo -u "$JAMIE_USERNAME" psql -d "$DB_NAME" -f db/db_init.sql; then
                        log "info" "Database schema initialized successfully"
                    else
                        log "error" "Failed to initialize database schema"
                        exit 1
                    fi
                else
                    log "error" "Failed to create database"
                    exit 1
                fi
            else
                log "error" "Failed to create database"
                exit 1
            fi
        fi
    fi
}

# Prompt for API keys
prompt_for_api_keys() {
    local services=("Discord" "Google Cloud (Gemini)" "Speechmatics")
    local keys=("DISCORD_TOKEN" "GEMINI_API_KEY" "SPEECHMATICS_API_KEY")

    for i in "${!services[@]}"; do
        read -p "Enter API key for ${services[$i]}: " value
        echo "Setting config: ${keys[$i]} = $value"
    done
}

# Main setup function
run_setup() {
    log "info" "Starting Jamie setup..."
    ensure_system_user
    ensure_database_user
    ensure_database
    log "info" "Simulating database opening."
    read -p "Succeed or fail? (s/f): " response
    if [[ $response == "s" ]]; then
        prompt_for_api_keys
        log "info" "Setup completed successfully!"
    else
        log "error" "Failed to open database"
        exit 1
    fi
}

# Run the setup
run_setup
